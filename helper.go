
/*
go-msgpack - Msgpack library for Go. Provides pack/unpack and net/rpc support.
https://github.com/ugorji/go-msgpack

Copyright (c) 2012, 2013 Ugorji Nwoke.
All rights reserved.

Redistribution and use in source and binary forms, with or without modification,
are permitted provided that the following conditions are met:

* Redistributions of source code must retain the above copyright notice,
  this list of conditions and the following disclaimer.
* Redistributions in binary form must reproduce the above copyright notice,
  this list of conditions and the following disclaimer in the documentation
  and/or other materials provided with the distribution.
* Neither the name of the author nor the names of its contributors may be used
  to endorse or promote products derived from this software
  without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE FOR
ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
(INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON
ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
*/

package msgpack

// Contains code shared by both encode and decode.

import (
	"unicode"
	"unicode/utf8"
	"reflect"
	"sync"
	"strings"
	"fmt"
	"sort"
	"time"
)

// A Container type specifies the different types of containers.
type containerType struct {
	cutoff int8
	b0, b1, b2 byte
}

var (
	containerRawBytes = containerType{32, 0xa0, 0xda, 0xdb}
	containerList = containerType{16, 0x90, 0xdc, 0xdd}
	containerMap = containerType{16, 0x80, 0xde, 0xdf}
)

var (
	structInfoFieldName = "_struct"
	
	cachedStructFieldInfos = make(map[reflect.Type]*structFieldInfos, 4)
	cachedStructFieldInfosMutex sync.RWMutex

	nilIntfSlice = []interface{}(nil)
	intfSliceTyp = reflect.TypeOf(nilIntfSlice)
	intfTyp = intfSliceTyp.Elem()
	byteSliceTyp = reflect.TypeOf([]byte(nil))
	ptrByteSliceTyp = reflect.TypeOf((*[]byte)(nil))
	mapStringIntfTyp = reflect.TypeOf(map[string]interface{}(nil))
	mapIntfIntfTyp = reflect.TypeOf(map[interface{}]interface{}(nil))
	timeTyp = reflect.TypeOf(time.Time{})
	ptrTimeTyp = reflect.TypeOf((*time.Time)(nil))
	int64SliceTyp = reflect.TypeOf([]int64(nil))
	
	intBitsize uint8 = uint8(reflect.TypeOf(int(0)).Bits())
	uintBitsize uint8 = uint8(reflect.TypeOf(uint(0)).Bits())
)

const (
	mapAccessThreshold = 4
	binarySearchThreshold = 9
)

type structFieldInfo struct {
	i         int      // field index in struct
	is        []int
	tag       string
	omitEmpty bool
	encName   string   // encode name
	encNameBs []byte
	name      string   // field name
	ikind     int      // kind of the field as an int i.e. int(reflect.Kind)
}

type structFieldInfos struct {
	sis []*structFieldInfo
}

type sfiSortedByEncName []*structFieldInfo

func (p sfiSortedByEncName) Len() int           { return len(p) }
func (p sfiSortedByEncName) Less(i, j int) bool { return p[i].encName < p[j].encName }
func (p sfiSortedByEncName) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func (c containerType) IsValid() bool {
	return c.b0 > 0
}

// return the field in struc for this fieldInfo.
func (si *structFieldInfo) field(struc reflect.Value) (rv reflect.Value) {
	if si.i > -1 {
		rv = struc.Field(si.i)
	} else {
		rv = struc.FieldByIndex(si.is)
	}
	return
}

func (sis *structFieldInfos) getForEncName(name string) (si *structFieldInfo) {
	sislen := len(sis.sis)
	if sislen < binarySearchThreshold {
		// linear search. faster than binary search in my testing up to 16-field structs.
		for i := 0; i < sislen; i++ {
			if sis.sis[i].encName == name {
				si = sis.sis[i]
				break
			}
		}
	} else {
		// binary search. adapted from sort/search.go.
		h, i, j := 0, 0, sislen
		for i < j {
			h = i + (j-i)/2 
			// i ≤ h < j
			if sis.sis[h].encName < name {
				i = h + 1 // preserves f(i-1) == false
			} else {
				j = h // preserves f(j) == true
			}
		}
		if i < sislen && sis.sis[i].encName == name {
			si = sis.sis[i]
		}
	}
	return
}

func getStructFieldInfos(rt reflect.Type) (sis *structFieldInfos) {
	cachedStructFieldInfosMutex.RLock()
	sis, ok := cachedStructFieldInfos[rt]
	cachedStructFieldInfosMutex.RUnlock()
	if ok {
		return 
	}
	
	cachedStructFieldInfosMutex.Lock()
	defer cachedStructFieldInfosMutex.Unlock()
	
	sis = new(structFieldInfos)
	
	var siInfo *structFieldInfo
	if f, ok := rt.FieldByName(structInfoFieldName); ok {
		siInfo = parseStructFieldInfo(structInfoFieldName, f.Tag.Get("msgpack"))
	}
	rgetStructFieldInfos(rt, nil, sis, siInfo)
	sort.Sort(sfiSortedByEncName(sis.sis))
	cachedStructFieldInfos[rt] = sis
	return
}

func rgetStructFieldInfos(rt reflect.Type, indexstack []int, sis *structFieldInfos, siInfo *structFieldInfo) {
	for j := 0; j < rt.NumField(); j++ {
		f := rt.Field(j)
		stag := f.Tag.Get("msgpack")
		if stag == "-" {
			continue
		}

		if r1, _ := utf8.DecodeRuneInString(f.Name); r1 == utf8.RuneError || !unicode.IsUpper(r1) {
			continue
		} 

		if f.Anonymous {
			//if anonymous, inline it if there is no msgpack tag, else treat as regular field
			if stag == "" {
				rgetStructFieldInfos(f.Type, append2Is(indexstack, j), sis, siInfo)
				continue
			}
		}
		si := parseStructFieldInfo(f.Name, stag)
		si.ikind = int(f.Type.Kind())
		if len(indexstack) == 0 {
			si.i = j
		} else {
			si.i = -1
			si.is = append2Is(indexstack, j)
		}

		if siInfo != nil {
			if siInfo.omitEmpty {
				si.omitEmpty = true
			}
		}
		sis.sis = append(sis.sis, si)
	}
}

func append2Is(indexstack []int, j int) (indexstack2 []int) {
	// istack2 := indexstack //make copy (not sufficient ... since it'd still share array)
	indexstack2 = make([]int, len(indexstack)+1)
	copy(indexstack2, indexstack)
	indexstack2[len(indexstack2)-1] = j
	return
}

func parseStructFieldInfo(fname string, stag string) (si *structFieldInfo) {
	if fname == "" {
		panic("parseStructFieldInfo: No Field Name")
	}
	si = &structFieldInfo {
		name: fname,
		encName: fname,
		tag: stag,
	}	
	
	if stag != "" {
		for i, s := range strings.Split(si.tag, ",") {
			if i == 0 {
				if s != "" {
					si.encName = s
				}
			} else {
				if s == "omitempty" {
					si.omitEmpty = true
				}
			}
		}
	}
	si.encNameBs = []byte(si.encName)
	return
}

func panicToErr(err *error) {
	if x := recover(); x != nil { 
		panicValToErr(x, err)
	}
}

func doPanic(tag string, format string, params []interface{}) {
	params2 := make([]interface{}, len(params) + 1)
	params2[0] = tag
	copy(params2[1:], params)
	panic(fmt.Errorf("%s: " + format, params2...))
}

