
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

import (
	"io"
	"bytes"
	"reflect"
	"math"
	"encoding/binary"
	"time"
)

// Some tagging information for error messages.
const msgTagEnc = "msgpack.encoder"

// An Encoder writes an object to an output stream in the msgpack format.
type Encoder struct {
	w io.Writer
	// temp byte array and slices used to prevent constant re-slicing while writing.
	x [16]byte        
	t1, t2, t3, t31, t4, t42, t5, t51, t6, t62, t9, t91 []byte 
	opts *EncoderOptions
}

type encExtTagFn struct {
	fn func(reflect.Value) ([]byte, error)
	tag byte
}
 
type encExtTypeTagFn struct {
	rt reflect.Type
	encExtTagFn
}

type EncoderOptions struct {
	extFuncs map[reflect.Type] encExtTagFn
	exts []encExtTypeTagFn
}

// NewEncoderOptions returns a new initialized *EncoderOptions.
func NewEncoderOptions() (*EncoderOptions) {
	o := EncoderOptions{
		exts: make([]encExtTypeTagFn, 0, 8),
		extFuncs: make(map[reflect.Type] encExtTagFn, 8),
	}
	return &o
}

// AddExt registers a function to handle encoding a given type as an extension  
// with a specific specific tag byte. 
// To remove an extension, pass fn=nil.
func (o *EncoderOptions) AddExt(rt reflect.Type, tag byte, fn func(reflect.Value) ([]byte, error)) {
	delete(o.extFuncs, rt)
	if fn != nil {
		o.extFuncs[rt] = encExtTagFn{fn, tag}
	}
	if leno := len(o.extFuncs); leno > cap(o.exts) {
		o.exts = make([]encExtTypeTagFn, leno, (leno * 3 / 2))
	} else {
		o.exts = o.exts[0:leno]
	}
	var i int
	for k, v := range o.extFuncs {
		o.exts[i] = encExtTypeTagFn {k, v}
		i++
	}
}

func (o *EncoderOptions) tagFnForType(rt reflect.Type) (xfn encExtTagFn) {	
	if l := len(o.exts); l == 0 {
		return
	} else if l < mapAccessThreshold {
		for _, x := range o.exts {
			if x.rt == rt {
				return x.encExtTagFn
			}
		}
	} else {
		// For >= 4 elements, map constant cost less than iteration cost.
		return o.extFuncs[rt]
	}
	return
}

// NewEncoder returns an Encoder for encoding an object.
func NewEncoder(w io.Writer, o *EncoderOptions) (e *Encoder) {	
	if o == nil {
		o = NewEncoderOptions()
	}
	e = &Encoder{w:w, opts:o}
	e.t1, e.t2, e.t3, e.t31, e.t4, e.t42 = e.x[:1], e.x[:2], e.x[:3], e.x[1:3], e.x[:4], e.x[2:4]
	e.t5, e.t51, e.t6, e.t62, e.t9, e.t91 = e.x[:5], e.x[1:5], e.x[:6], e.x[2:6], e.x[:9], e.x[1:9]
	return
}

// Encode writes an object into a stream in the MsgPack format.
// 
// Struct values encode as maps. Each exported struct field is encoded unless:
//    - the field's tag is "-", or
//    - the field is empty and its tag specifies the "omitempty" option.
//
// The empty values are false, 0, any nil pointer or interface value, 
// and any array, slice, map, or string of length zero. 
// 
// Anonymous fields are encoded inline if no msgpack tag is present.
// Else they are encoded as regular fields.
// 
// The object's default key string is the struct field name but can be 
// specified in the struct field's tag value. 
// The "msgpack" key in struct field's tag value is the key name, 
// followed by an optional comma and options. 
// 
// To set an option on all fields (e.g. omitempty on all fields), you 
// can create a field called _struct, and set flags on it.
// 
// Examples:
//    
//      type MyStruct struct {
//          _struct bool    `msgpack:",omitempty"`   //set omitempty for every field
//          Field1 string   `msgpack:"-"`            //skip this field
//          Field2 int      `msgpack:"myName"`       //Use key "myName" in encode stream
//          Field3 int32    `msgpack:",omitempty"`   //use key "Field3". Omit if empty.
//          Field4 bool     `msgpack:"f4,omitempty"` //use key "f4". Omit if empty.
//          ...
//      }
//    
func (e *Encoder) Encode(v interface{}) (err error) {
	defer panicToErr(&err) 
	e.encode(v)
	return 
}

func (e *Encoder) encode(iv interface{}) {
	switch v := iv.(type) {
	case nil:
		e.encNil()
		
	case reflect.Value:
		e.encodeValue(v)

	case string:
		e.encString(v)
	case bool:
		e.encBool(v)
	case int:
		e.encInt(int64(v))
	case int8:
		e.encInt(int64(v))
	case int16:
		e.encInt(int64(v))
	case int32:
		e.encInt(int64(v))
	case int64:
		e.encInt(v)
	case uint:
		e.encUint(uint64(v))
	case uint8:
		e.encUint(uint64(v))
	case uint16:
		e.encUint(uint64(v))
	case uint32:
		e.encUint(uint64(v))
	case uint64:
		e.encUint(v)
	case float32:
		e.encFloat32(v)
	case float64:
		e.encFloat64(v)

	case *string:
		e.encString(*v)
	case *bool:
		e.encBool(*v)
	case *int:
		e.encInt(int64(*v))
	case *int8:
		e.encInt(int64(*v))
	case *int16:
		e.encInt(int64(*v))
	case *int32:
		e.encInt(int64(*v))
	case *int64:
		e.encInt(*v)
	case *uint:
		e.encUint(uint64(*v))
	case *uint8:
		e.encUint(uint64(*v))
	case *uint16:
		e.encUint(uint64(*v))
	case *uint32:
		e.encUint(uint64(*v))
	case *uint64:
		e.encUint(*v)
	case *float32:
		e.encFloat32(*v)
	case *float64:
		e.encFloat64(*v)

	default:
		e.encodeValue(reflect.ValueOf(iv))
	}
	
}

func (e *Encoder) encodeValue(rv reflect.Value) {
	rt := rv.Type()
	rk := rv.Kind()
	if xf := e.opts.tagFnForType(rt); xf.fn != nil {
		switch rk {
		case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice:
			if rv.IsNil() {
				e.encNil()
				return
			}
		}
		bs, fnerr := xf.fn(rv)
		if fnerr != nil {
			panic(fnerr)
		}
		e.encExtBytes(xf.tag, bs)
		return
	}
	
	// ensure more common cases appear early in switch.
	switch rk {
	case reflect.Bool:
		e.encBool(rv.Bool())
	case reflect.String:
		e.encString(rv.String())
	case reflect.Float64:
		e.encFloat64(rv.Float())
	case reflect.Float32:
		e.encFloat32(float32(rv.Float()))
	case reflect.Slice:
		if rv.IsNil() {
			e.encNil()
			break
		} 
		if rt == byteSliceTyp {
			e.encRaw(rv.Bytes())
			break
		}
		l := rv.Len()
		e.writeContainerLen(containerList, l)
		if l == 0 {
			break
		}
		for j := 0; j < l; j++ {
			e.encodeValue(rv.Index(j))
		}
	case reflect.Array:
		e.encodeValue(rv.Slice(0, rv.Len()))
	case reflect.Map:
		if rv.IsNil() {
			e.encNil()
			break
		}
		l := rv.Len()
		e.writeContainerLen(containerMap, l)
		if l == 0 {
			break
		}
		
		for _, mk := range rv.MapKeys() {
			e.encodeValue(mk)
			e.encodeValue(rv.MapIndex(mk))
		}
	case reflect.Struct:
		e.encodeStruct(rt, rv)
	case reflect.Ptr:
		if rv.IsNil() {
			e.encNil()
			break
		}
		e.encodeValue(rv.Elem())
	case reflect.Interface:
		if rv.IsNil() {
			e.encNil()
			break
		}
		e.encodeValue(rv.Elem())
	case reflect.Int, reflect.Int8, reflect.Int64, reflect.Int32, reflect.Int16:
		e.encInt(rv.Int())
	case reflect.Uint8, reflect.Uint64, reflect.Uint, reflect.Uint32, reflect.Uint16:
		e.encUint(rv.Uint())
	case reflect.Invalid:
		e.encNil()
	default:
		e.err("Unsupported kind: %s, for: %#v", rk, rv)
	}
	return
}

func (e *Encoder) writeContainerLen(ct containerType, l int) {
	switch {
	case l < int(ct.cutoff):
		e.t1[0] = (ct.b0 | byte(l))
		e.writeb(1, e.t1)
	case l < 65536:
		e.t3[0] = ct.b1
		binary.BigEndian.PutUint16(e.t31, uint16(l))
		e.writeb(3, e.t3)
	default:
		e.t5[0] = ct.b2
		binary.BigEndian.PutUint32(e.t51, uint32(l))
		e.writeb(5, e.t5)
	}
}

func (e *Encoder) encNil() {
	e.t1[0] = 0xc0
	e.writeb(1, e.t1)
}

func (e *Encoder) encInt(i int64) {
	switch {
	case i >= -32 && i <= math.MaxInt8:
		e.t1[0] = byte(i)
		e.writeb(1, e.t1)
	case i < -32 && i >= math.MinInt8:
		e.t2[0], e.t2[1] = 0xd0, byte(i)
		e.writeb(2, e.t2)
	case i >= math.MinInt16 && i <= math.MaxInt16:
		e.t3[0] = 0xd1
		binary.BigEndian.PutUint16(e.t31, uint16(i))
		e.writeb(3, e.t3)
	case i >= math.MinInt32 && i <= math.MaxInt32:
		e.t5[0] = 0xd2
		binary.BigEndian.PutUint32(e.t51, uint32(i))
		e.writeb(5, e.t5)
	case i >= math.MinInt64 && i <= math.MaxInt64:
		e.t9[0] = 0xd3
		binary.BigEndian.PutUint64(e.t91, uint64(i))
		e.writeb(9, e.t9)
	default:
		e.err("encInt64: Unreachable block")
	}
}

func (e *Encoder) encUint(i uint64) {
	switch {
	case i <= math.MaxInt8:
		e.t1[0] = byte(i)
		e.writeb(1, e.t1)
	case i <= math.MaxUint8:
		e.t2[0], e.t2[1] = 0xcc, byte(i)
		e.writeb(2, e.t2)
	case i <= math.MaxUint16:
		e.t3[0] = 0xcd
		binary.BigEndian.PutUint16(e.t31, uint16(i))
		e.writeb(3, e.t3)
	case i <= math.MaxUint32:
		e.t5[0] = 0xce
		binary.BigEndian.PutUint32(e.t51, uint32(i))
		e.writeb(5, e.t5)
	default:
		e.t9[0] = 0xcf
		binary.BigEndian.PutUint64(e.t91, i)
		e.writeb(9, e.t9)
	}
}

func (e *Encoder) encBool(b bool) {
	if b {
		e.t1[0] = 0xc3
	} else {
		e.t1[0] = 0xc2
	}
	e.writeb(1, e.t1)
}

func (e *Encoder) encFloat32(f float32) {
	e.t5[0] = 0xca
	binary.BigEndian.PutUint32(e.t51, math.Float32bits(f))
	e.writeb(5, e.t5)
}

func (e *Encoder) encFloat64(f float64) {
	e.t9[0] = 0xcb
	binary.BigEndian.PutUint64(e.t91, math.Float64bits(f))
	e.writeb(9, e.t9)
}


func (e *Encoder) encodeStruct(rt reflect.Type, rv reflect.Value) {
	sis := getStructFieldInfos(rt)
	// encNames := make([]string, len(sis.sis))
	rvals := make([]reflect.Value, len(sis.sis))
	sivals := make([]int, len(sis.sis))
	newlen := 0
	for i, si := range sis.sis {
		rval0 := si.field(rv)
		if si.omitEmpty && isEmptyValue(rval0) {
			continue
		}
		// encNames[newlen] = si.encName // si.encNameBs
		rvals[newlen] = rval0
		sivals[newlen] = i
		newlen++
	}
	
	e.writeContainerLen(containerMap, newlen)
	for j := 0; j < newlen; j++ {
		e.encString(sis.sis[sivals[j]].encName)
		e.encodeValue(rvals[j])
	}	
}

func (e *Encoder) encString(s string) {
	numbytes := len(s)
	e.writeContainerLen(containerRawBytes, numbytes)
	// e.encode([]byte(s)) // using io.WriteString is faster
	n, err := io.WriteString(e.w, s)
	if err != nil {
		panic(err)
	}
	if n != numbytes {
		e.err("write: Incorrect num bytes written. Expecting: %v, Wrote: %v", numbytes, n)
	}
}

func (e *Encoder) encRaw(bs []byte) {
	l := len(bs)
	e.writeContainerLen(containerRawBytes, l)
	if l > 0 {
		e.writeb(l, bs)
	}
}

func (e *Encoder) encExtBytes(xtag byte, bs []byte) {
	l := len(bs)
	e.x[1] = xtag
	switch {
	case l <= 4:
		e.x[0] = 0xd4 | byte(l)
		e.x[1] = xtag
		e.writeb(2, e.t2)
	case l <= 8:
		e.x[0] = 0xc0 | byte(l)
		e.writeb(2, e.t2)
	case l < 256:
		e.x[0] = 0xc9
		e.x[2] = byte(l)
		e.writeb(3, e.t3)	
	case l < 65536:
		e.x[0] = 0xd8
		binary.BigEndian.PutUint16(e.t42, uint16(l))
		e.writeb(4, e.t4)	
	default:
		e.x[0] = 0xd9
		binary.BigEndian.PutUint32(e.t62, uint32(l))
		e.writeb(6, e.t6)		
	}
	e.writeb(l, bs)
}

func (e *Encoder) writeb(numbytes int, bs []byte) {
	n, err := e.w.Write(bs)
	if err != nil {
		panic(err)
	}
	if n != numbytes {
		e.err("write: Incorrect num bytes written. Expecting: %v, Wrote: %v", numbytes, n)
	}
}
	
func (e *Encoder) err(format string, params ...interface{}) {
	doPanic(msgTagEnc, format, params)
}

func EncodeTimeExt(rv reflect.Value) ([]byte, error) {
	t := rv.Interface().(time.Time)
	
	buf := new(bytes.Buffer)
	e2 := NewEncoder(buf, nil)
	
	e2.encInt(t.Unix())
	if t.Location() == time.UTC {
		if t.Nanosecond() != 0 {
			e2.encInt(int64(t.Nanosecond()))
		}
	} else {
		e2.encInt(int64(t.Nanosecond()))
		_, zoneOffset := t.Zone()
		e2.encInt((int64(zoneOffset) / 60 / 15) + 48)
	}
	return buf.Bytes(), nil
}

func EncodeBinaryExt(rv reflect.Value) ([]byte, error) {
	return rv.Interface().([]byte), nil
}

// Marshal is a convenience function which encodes v to a stream of bytes. 
// It delegates to Encoder.Encode.
func Marshal(v interface{}, o *EncoderOptions) (b []byte, err error) {
	bs := new(bytes.Buffer)
	if err = NewEncoder(bs, o).Encode(v); err == nil {
		b = bs.Bytes()
	}
	return
}

