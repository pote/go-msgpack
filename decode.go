
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
	"fmt"
	"encoding/binary"
	"time"
)

// Some tagging information for error messages.
var (
	_ = fmt.Printf
	msgTagDec = "msgpack.decoder"
	msgBadDesc = "Unrecognized descriptor byte"
)

type decNil struct { }
type decExt struct { }

// A Decoder reads and decodes an object from an input stream in the msgpack format.
type Decoder struct {
	bd byte
	bdRead bool
	eof bool
	r io.Reader
	opts *DecoderOptions
	x [16]byte        //temp byte array re-used internally for efficiency
	t1, t2, t4, t8 []byte // use these, so no need to constantly re-slice
}

type decExtTagFn struct {
	fn func(reflect.Value, []byte)(error)
	tag byte
}

type decExtTypeTagFn struct {
	rt reflect.Type
	decExtTagFn
}

type DecoderOptions struct {
	// MapType determines how we initialize a map that we detect in the stream,
	// when decoding into a nil interface{}.
	MapType reflect.Type
	// SliceType controls how we initializa a slice that we detect in the stream,
	// when decoding into a nil interface{}.
	SliceType reflect.Type
	// RawToString controls how raw bytes are decoded into a nil interface{}.
	// Note that setting an extension func for []byte ensures that raw bytes are decoded as strings,
	// regardless of this setting. This setting is used only if an extension func isn't defined for []byte.
	RawToString bool

	// if an extension for byte slice is defined, then always decode Raw as strings
	rawToStringOverride bool
	exts []decExtTypeTagFn
	extFuncs map[reflect.Type] decExtTagFn
}

// Creates a new DecoderOptions with:
//   MapType: map[interface{}]interface{} 
//   SliceType: []interface{}
//   RawToString: true
func NewDecoderOptions() (*DecoderOptions) {
	o := DecoderOptions{
		MapType: mapIntfIntfTyp,
		SliceType: intfSliceTyp,
		RawToString: true,
		exts: make([]decExtTypeTagFn, 0, 1),
		extFuncs: make(map[reflect.Type]decExtTagFn, 8),
	}
	return &o
}

// AddExt registers a function to handle decoding into a given type when an extension type 
// and specific tag byte is detected in the msgpack stream. 
// To remove an extension, pass fn=nil.
func (o *DecoderOptions) AddExt(rt reflect.Type, tag byte, fn func(reflect.Value, []byte) (error)) {
	if _, ok := o.extFuncs[rt]; ok {
		delete(o.extFuncs, rt)
		if rt == byteSliceTyp {
			o.rawToStringOverride = false
		}
	}
	if fn != nil {
		o.extFuncs[rt] = decExtTagFn{fn, tag}
		if rt == byteSliceTyp {
			o.rawToStringOverride = true
		}
	}
	
	if leno := len(o.extFuncs); leno > cap(o.exts) {
		o.exts = make([]decExtTypeTagFn, leno, (leno * 3 / 2))
	} else {
		o.exts = o.exts[0:leno]
	}
	var i int
	for k, v := range o.extFuncs {
		o.exts[i] = decExtTypeTagFn {k, v}
		i++
	}
}

func (o *DecoderOptions) typeForTag(tag byte) reflect.Type {
	for _, x := range o.exts {
		if x.tag == tag {
			return x.rt
		}
	}
	return nil
}

func (o *DecoderOptions) tagFnForType(rt reflect.Type) (xfn decExtTagFn) {
	// For >= 4 elements, map outways cost of iteration.
	if len(o.exts) > 3 {
		return o.extFuncs[rt]
	}
	for _, x := range o.exts {
		if x.rt == rt {
			return x.decExtTagFn
		}
	}
	return
}

// NewDecoder returns a Decoder for decoding a stream of bytes into an object.
func NewDecoder(r io.Reader, o *DecoderOptions) (d *Decoder) {
	if o == nil {
		o = NewDecoderOptions()
	}
	d = &Decoder{ r:r, bdRead: false, opts: o }
	d.t1, d.t2, d.t4, d.t8 = d.x[:1], d.x[:2], d.x[:4], d.x[:8]
	return
}

// Decode decodes the stream from reader and stores the result in the 
// value pointed to by v. v cannot be a nil pointer. v can also be 
// a reflect.Value of a pointer.
// 
// Note that a pointer to a nil interface is not a nil pointer.
// If you do not know what type of stream it is, pass in a pointer to a nil interface.
// We will decode and store a value in that nil interface. 
// 
// Sample usages:
//   // Decoding into a non-nil typed value
//   var f float32
//   err = msgpack.NewDecoder(r, nil).Decode(&f)
//
//   // Decoding into nil interface
//   var v interface{}
//   dec := msgpack.NewDecoder(r, nil)
//   err = dec.Decode(&v)
//   
func (d *Decoder) Decode(v interface{}) (err error) {
	defer panicToErr(&err)
	d.decode(v)
	return
}

func (d *Decoder) decode(iv interface{}) {
	d.ensureBdRead()
	
	switch v := iv.(type) {
	case nil:
		d.err("Cannot decode into nil.")
	case reflect.Value:
		d.chkPtrValue(v)
		d.decodeValue(v)
	case *string:
		*v = d.decodeString()
	case *[]byte:
		if bs2, changed2 := d.decodeBytes(*v); changed2 {
			*v = bs2
		}
	case *bool:
		*v = d.decodeBool()
	case *int:
		*v = int(d.decodeInt(intBitsize))
	case *int8:
		*v = int8(d.decodeInt(8))
	case *int16:
		*v = int16(d.decodeInt(16))
	case *int32:
		*v = int32(d.decodeInt(32))
	case *int64:
		*v = int64(d.decodeInt(64))
	case *uint:
		*v = uint(d.decodeUint(uintBitsize))
	case *uint8:
		*v = uint8(d.decodeUint(8))
	case *uint16:
		*v = uint16(d.decodeUint(16))
	case *uint32:
		*v = uint32(d.decodeUint(32))
	case *uint64:
		*v = uint64(d.decodeUint(64))
	case *float32:
		*v = float32(d.decodeFloat(true))
	case *float64:
		*v = d.decodeFloat(false) 
	case *interface{}:
	 	d.decodeValue(reflect.ValueOf(iv).Elem())
	default:
		rv := reflect.ValueOf(iv)
		d.chkPtrValue(rv)
		d.decodeValue(rv)
	}	
}


// Note: This returns either a primitive (int, bool, etc) for non-containers,
// or a containerType, or a specific type denoting nil or extension. 
// It is called when a nil interface{} is passed, leaving it up to the Decoder
// to introspect the stream and decide how best to decode.
// It deciphers the value by looking at the stream first.
func (d *Decoder) decodeNaked() (v interface{}) {
	d.ensureBdRead()
	bd := d.bd

	switch bd {
	case 0xc0:
		v = decNil{}
	case 0xc2:
		v = false
	case 0xc3:
		v = true

	case 0xca:
		v = math.Float32frombits(d.readUint32())
	case 0xcb:
		v = math.Float64frombits(d.readUint64())
		
	case 0xcc:
		v = d.readUint8()
	case 0xcd:
		v = d.readUint16()
	case 0xce:
		v = d.readUint32()
	case 0xcf:
		v = d.readUint64()
		
	case 0xd0:
		v = int8(d.readUint8())
	case 0xd1:
		v = int16(d.readUint16())
	case 0xd2:
		v = int32(d.readUint32())
	case 0xd3:
		v = int64(d.readUint64())
		
	default:
		switch {
		case bd >= 0xe0 && bd <= 0xff, bd >= 0x00 && bd <= 0x7f:
			// fixnum
			v = int8(bd)
			
		case bd == 0xda, bd == 0xdb, bd >= 0xa0 && bd <= 0xbf:
			v = containerRawBytes
		case bd == 0xdc, bd == 0xdd, bd >= 0x90 && bd <= 0x9f:
			v = containerList
		case bd == 0xde, bd == 0xdf, bd >= 0x80 && bd <= 0x8f:
			v = containerMap

		case bd >= 0xc4 && bd <= 0xc9, bd >= 0xd4 && bd <= 0xd9:
			v = decExt{}
			
		default:
			d.err("Nil-Deciphered DecodeValue: %s: hex: %x, dec: %d", msgBadDesc, bd, bd)
		}
	}
	return
}

func (d *Decoder) decodeValue(rv reflect.Value) {
	// Note: if stream is set to nil (0xc0), we set the corresponding value to its "zero" value
	
	// var ctr int (define this above the  function if trying to do this run)
	// ctr++
	// log(".. [%v] enter decode: rv: %v <==> %T <==> %v", ctr, rv, rv.Interface(), rv.Interface())
	// defer func(ctr2 int) {
	// 	log(".... [%v] exit decode: rv: %v <==> %T <==> %v", ctr2, rv, rv.Interface(), rv.Interface())
	// }(ctr)
	d.ensureBdRead()
	
	rvOrig := rv
	rk := rv.Kind()
	wasNilIntf := rk == reflect.Interface && rv.IsNil()
	var xtagRead bool
	var rt reflect.Type
	
	//if nil interface, use some hieristics to set the nil interface to an 
	//appropriate value based on the first byte read (byte descriptor bd)
	if wasNilIntf {
		v := d.decodeNaked()
		switch v2 := v.(type) {
		case containerType:
			switch v2 {
			case containerRawBytes:
				if d.opts.rawToStringOverride || d.opts.RawToString {
					var rvm string
					rv = reflect.ValueOf(&rvm).Elem()
				} else {
					rv = reflect.New(byteSliceTyp).Elem() // Use New, not Zero, so it's settable
				}
			case containerList:
				rv = reflect.New(d.opts.SliceType).Elem()
			case containerMap:
				rv = reflect.MakeMap(d.opts.MapType)
			default:
				d.err("Unknown container type: %v", v2)
			}
			rk = rv.Kind()
			rt = rv.Type()
		case decNil:
			d.bdRead = false
			rt = rv.Type()
		case decExt:
			xtag := d.readUint8()
			xtagRead = true
			if rt = d.opts.typeForTag(xtag); rt != nil {
				if rt.Kind() == reflect.Ptr {
					rv = reflect.New(rt.Elem())
				} else {
					rv = reflect.New(rt).Elem()
				}
				break
			} 
			d.err("Unable to find type mapped to extension tag: %v", xtag)
		default:
			// all low level primitives ... 
			rvOrig.Set(reflect.ValueOf(v))
			d.bdRead = false
			return
		}
	}
	
	if rt == nil {
		rt = rv.Type()
	}
	if d.bd == 0xc0 {
		d.bdRead = false
		// log("decoding value as nil: %v", rt)
		rv.Set(reflect.Zero(rt))
		return
	}
	
	// check extensions:
	// quick check first to see if byte desc matches (since checking maps expensive).
	// We do this here, because an extension can be registered for any type, regardless of the 
	// Kind (e.g. type BitSet int64, type MyStruct { /* unexported fields */ }, type X []int, etc.
	
	// We cannot check stream bytes here, because the current value may be a pointer,
	// whereas the user registered the non-pointer type (so when we call recursive func again, it works).
	// if (d.bd >= 0xc4 && d.bd <= 0xc9) || (d.bd >= 0xd4 && d.bd <= 0xd9) {
	// }
	if bfn := d.opts.tagFnForType(rt); bfn.fn != nil {
		if !xtagRead {
			xtag := d.readUint8()
			if bfn.tag != xtag {
				d.err("Wrong extension tag: %b, found for type: %v. Expecting: %v", xtag, rt, bfn.tag)
			}
		}
		clen := d.readExtLen()
		xbs := make([]byte, clen)
		d.readb(clen, xbs)
		if fnerr := bfn.fn(rv, xbs); fnerr != nil {
			panic(fnerr)
		}
		d.bdRead = false
		if wasNilIntf {
			rvOrig.Set(rv)
		}
		return
	}
	
	// use this variable and function to reset rv if necessary.
	var rvn reflect.Value
	rvReset := func() {
		if wasNilIntf {
			rv = rvn
		} else {
			rv.Set(rvn)
		}
	}
	
	// (Mar 7, 2013. DON'T REARRANGE ... code clarity)
	// tried arranging in sequence of most probable ones. 
	// string, bool, integer, float, struct, ptr, slice, array, map, interface, uint.
	switch rk {
	case reflect.String:
		rv.SetString(d.decodeString())
	case reflect.Bool:
		rv.SetBool(d.decodeBool())
	case reflect.Int:
		rv.SetInt(d.decodeInt(intBitsize))
	case reflect.Int64:
		rv.SetInt(d.decodeInt(64))
	case reflect.Int32:
		rv.SetInt(d.decodeInt(32))
	case reflect.Int8:
		rv.SetInt(d.decodeInt(8))
	case reflect.Int16:
		rv.SetInt(d.decodeInt(16))
	case reflect.Float32:
		rv.SetFloat(d.decodeFloat(true))
	case reflect.Float64:
		rv.SetFloat(d.decodeFloat(false))
	case reflect.Uint8:
		rv.SetUint(d.decodeUint(8))
	case reflect.Uint64: 
		rv.SetUint(d.decodeUint(64))
	case reflect.Uint:
		rv.SetUint(d.decodeUint(uintBitsize))
	case reflect.Uint32:
		rv.SetUint(d.decodeUint(32))
	case reflect.Uint16:
		rv.SetUint(d.decodeUint(16))
	case reflect.Ptr:
		if rv.IsNil() {
			rvn = reflect.New(rt.Elem())
			rvReset()
		}
		d.decodeValue(rv.Elem())
	case reflect.Interface:
		d.decodeValue(rv.Elem())
	case reflect.Struct:
		containerLen := d.readContainerLen(containerMap)
		d.bdRead = false
		
		// if stream had nil, reset the struct
		if containerLen < 0 {
			rvn = reflect.Zero(rt)
			rvReset()
			break
		} 
		if containerLen == 0 {
			break
		}
		
		sfi := getStructFieldInfos(rt)
		for j := 0; j < containerLen; j++ {
			var rvkencname string
			d.decode(&rvkencname)
			rvksi := sfi.getForEncName(rvkencname)
			if rvksi == nil {
				var nilintf0 interface{}
				d.decodeValue(reflect.ValueOf(&nilintf0).Elem())
			} else { 
				d.decodeValue(rvksi.field(rv))
			}
		}
	case reflect.Slice:
		if rt == byteSliceTyp { // rawbytes 
			if bs2, changed2 := d.decodeBytes(rv.Bytes()); changed2 {
				rv.SetBytes(bs2)
			}
			break			
		}
		
		containerLen := d.readContainerLen(containerList)
		d.bdRead = false

		if containerLen < 0 {
			rvn = reflect.Zero(rt)
			rvReset()
			break
		}
		if containerLen == 0 {
			break
		}
		
		if rv.IsNil() {
			rvn = reflect.MakeSlice(rt, containerLen, containerLen)
			rvReset()
		} else {
			rvlen := rv.Len()
			if containerLen > rv.Cap() {
				rvn = reflect.MakeSlice(rt, containerLen, containerLen)
				if rvlen > 0 {
					reflect.Copy(rvn, rv)
				}
				rvReset()
			} else if containerLen > rvlen {
				rv.SetLen(containerLen)
			}
		}

		for j := 0; j < containerLen; j++ {
			d.decodeValue(rv.Index(j))
		}
	case reflect.Array:
		rvlen := rv.Len()
		rvelemtype := rt.Elem()
		rvelemkind := rvelemtype.Kind()
		if rvelemkind == reflect.Uint8 { // rawbytes
			containerLen := d.readContainerLen(containerRawBytes)
			var bs []byte = rv.Slice(0, rvlen).Bytes()
			if containerLen < 0 {
				// clear
				for j := 0; j < rvlen; j++ {
					bs[j] = 0
				}
			} else if containerLen == 0 {
			} else if rvlen == containerLen {
				d.readb(containerLen, bs)
			} else if rvlen > containerLen {
				d.readb(containerLen, bs[:containerLen])
			} else {
				d.err("Array len: %d must be >= container Len: %d", rvlen, containerLen)
			} 
			d.bdRead = false
			break
		}
		
		containerLen := d.readContainerLen(containerList)
		d.bdRead = false
		if containerLen == 0 {
			break
		}
		if containerLen < 0 {
			containerLen = 0
		}
		if rvlen < containerLen {
			d.err("Array len: %d must be >= container Len: %d", rvlen, containerLen)
		} else if rvlen > containerLen {
			rvelemzero := reflect.Zero(rvelemtype)
			for j := containerLen; j < rvlen; j++ {
				rv.Index(j).Set(rvelemzero)
			}
		}
		
		for j := 0; j < containerLen; j++ {
			d.decodeValue(rv.Index(j))
		}
	case reflect.Map:
		containerLen := d.readContainerLen(containerMap)
		d.bdRead = false
		
		if containerLen == 0 {
			break
		}
		if containerLen < 0 {
			rvn = reflect.Zero(rt)
			rvReset()
			break
		}
		
		if rv.IsNil() {
			rvn = reflect.MakeMap(rt)
			rvReset()
		}
		ktype, vtype := rt.Key(), rt.Elem()			
		for j := 0; j < containerLen; j++ {
			rvk := reflect.New(ktype).Elem()
			d.decodeValue(rvk)
			
			if ktype == intfTyp {
				rvk = rvk.Elem()
				if rvk.Type() == byteSliceTyp {
					rvk = reflect.ValueOf(string(rvk.Bytes()))
				}
			}
			rvv := rv.MapIndex(rvk)
			if !rvv.IsValid() {
				rvv = reflect.New(vtype).Elem()
			}
			
			d.decodeValue(rvv)
			rv.SetMapIndex(rvk, rvv)
		}
	default:
		d.err("Unhandled single-byte value: %s: %x", msgBadDesc, d.bd)
	}
	
	if wasNilIntf {
		// if setToIndir { rvOrig.Set(rv.Elem()) } else { rvOrig.Set(rv) }
		rvOrig.Set(rv)
	}

	return
}

// int can be decoded from msgpack type: intXXX or uintXXX 
func (d *Decoder) decodeInt(bitsize uint8) (i int64) {
	switch d.bd {
	case 0xcc:
		i = int64(uint64(d.readUint8()))
	case 0xcd:
		i = int64(uint64(d.readUint16()))
	case 0xce:
		i = int64(uint64(d.readUint32()))
	case 0xcf:
		i = int64(d.readUint64())
	case 0xd0:
		i = int64(int8(d.readUint8()))
	case 0xd1:
		i = int64(int16(d.readUint16()))
	case 0xd2:
		i = int64(int32(d.readUint32()))
	case 0xd3:
		i = int64(d.readUint64())
	default:
		switch {
		case d.bd >= 0x00 && d.bd <= 0x7f:
			i = int64(int8(d.bd))
		case d.bd >= 0xe0 && d.bd <= 0xff:
			i = int64(int8(d.bd))
		default:
			d.err("Unhandled single-byte unsigned integer value: %s: %x", msgBadDesc, d.bd)
		}
	}
	// check overflow (logic adapted from std pkg reflect/value.go OverflowUint()
	if bitsize > 0 {
		if trunc := (i << (64 - bitsize)) >> (64 - bitsize); i != trunc {
			d.err("Overflow int value: %v", i)
		}
	}
	d.bdRead = false
	return
}


// uint can be decoded from msgpack type: intXXX or uintXXX 
func (d *Decoder) decodeUint(bitsize uint8) (ui uint64) {
	switch d.bd {
	case 0xcc:
		ui = uint64(d.readUint8())
	case 0xcd:
		ui = uint64(d.readUint16())
	case 0xce:
		ui = uint64(d.readUint32())
	case 0xcf:
		ui = d.readUint64()
	case 0xd0:
		i := int64(int8(d.readUint8()))
		if i >= 0 {
			ui = uint64(i)
		} else {
			d.err("Assigning negative signed value: %v, to unsigned type", i)
		}
	case 0xd1:
		i := int64(int16(d.readUint16()))
		if i >= 0 {
			ui = uint64(i)
		} else {
			d.err("Assigning negative signed value: %v, to unsigned type", i)
		}
	case 0xd2:
		i := int64(int32(d.readUint32()))
		if i >= 0 {
			ui = uint64(i)
		} else {
			d.err("Assigning negative signed value: %v, to unsigned type", i)
		}
	case 0xd3:
		i := int64(d.readUint64())
		if i >= 0 {
			ui = uint64(i)
		} else {
			d.err("Assigning negative signed value: %v, to unsigned type", i)
		}
	default:
		switch {
		case d.bd >= 0x00 && d.bd <= 0x7f:
			ui = uint64(d.bd)
		case d.bd >= 0xe0 && d.bd <= 0xff:
			d.err("Assigning negative signed value: %v, to unsigned type", int(d.bd))
		default:
			d.err("Unhandled single-byte unsigned integer value: %s: %x", msgBadDesc, d.bd)
		}
	}
	// check overflow (logic adapted from std pkg reflect/value.go OverflowUint()
	if bitsize > 0 {
		if trunc := (ui << (64 - bitsize)) >> (64 - bitsize); ui != trunc {
			d.err("Overflow uint value: %v", ui)
		}
	}
	d.bdRead = false
	return
}

// float can either be decoded from msgpack type: float, double or intX
func (d *Decoder) decodeFloat(chkOverflow32 bool) (f float64) {
	switch d.bd {
	case 0xca:
		f = float64(math.Float32frombits(d.readUint32()))
	case 0xcb:
		f = math.Float64frombits(d.readUint64())
	default:
		f = float64(d.decodeInt(0))
	}
	// check overflow (logic adapted from std pkg reflect/value.go OverflowFloat()
	if chkOverflow32 {
		f2 := f
		if f2 < 0 {
			f2 = -f
		}
		if math.MaxFloat32 < f2 && f2 <= math.MaxFloat64 {
			d.err("Overflow float32 value: %v", f2)
		}
	}
	d.bdRead = false
	return
}

// bool can be decoded from bool, fixnum 0 or 1.
func (d *Decoder) decodeBool() (b bool) {
	switch d.bd {
	case 0xc2, 0x00:
		// b = false
	case 0xc3, 0x01:
		b = true
	default:
		d.err("Invalid single-byte value for bool: %s: %x", msgBadDesc, d.bd)
	}
	d.bdRead = false
	return
}
	
func (d *Decoder) decodeString() (s string) {
	clen := d.readContainerLen(containerRawBytes)
	if clen > 0 {
		bs := make([]byte, clen)
		d.readb(clen, bs)
		s = string(bs)
	}
	d.bdRead = false
	return
}

// Return changed=true if length of passed slice is less than length of bytes in the stream.
// Callers must check if changed=true (to decide whether to replace the one they have)
func (d *Decoder) decodeBytes(bs []byte) (bsOut []byte, changed bool) {
	clen := d.readContainerLen(containerRawBytes)
	if clen < 0 {
		changed = true
	}
	// if no contents in stream, don't update the passed byteslice
	if clen > 0 {
		rvlen := len(bs)
		if rvlen == clen {
		} else if rvlen > clen {
			bs = bs[:clen]
		} else {
			bs = make([]byte, clen)
			bsOut = bs
			changed = true
		}
		d.readb(clen, bs)
	}
	d.bdRead = false
	return
}

// Every top-level decode funcs (i.e. decodeValue, decode) must call this first.
func (d *Decoder) ensureBdRead() {
	if d.bdRead {
		return
	}
	d.readb(1, d.t1)
	d.bd = d.t1[0]
	d.bdRead = true
}

// called only before trying to decode again using low-level decode.
// Used by built-in extension decoders where they don't know exactly how many 
// encoded values in stream.
func (d *Decoder) checkEOF() bool {
	if d.eof {
		return true
	}
	_, err := io.ReadAtLeast(d.r, d.t1, 1) 
	if err == io.EOF {
		d.eof = true
		return true
	}
	if err != nil {
		panic(err)
	}
	d.bd = d.t1[0]
	d.bdRead = true
	return false
}

// read a number of bytes into bs
func (d *Decoder) readb(numbytes int, bs []byte) {
	n, err := io.ReadAtLeast(d.r, bs, numbytes) 
	if err != nil {
		panic(err)
	} else if n != numbytes {
		d.err("read: Incorrect num bytes read. Expecting: %v, Received: %v", numbytes, n)
	}
}

func (d *Decoder) readUint8() uint8 {
	d.readb(1, d.t1)
	return d.t1[0]
}

func (d *Decoder) readUint16() uint16 {
	d.readb(2, d.t2)
	return binary.BigEndian.Uint16(d.t2)
}

func (d *Decoder) readUint32() uint32 {
	d.readb(4, d.t4)
	return binary.BigEndian.Uint32(d.t4)
}

func (d *Decoder) readUint64() uint64 {
	d.readb(8, d.t8)
	return binary.BigEndian.Uint64(d.t8)
}

func (d *Decoder) readContainerLen(ct containerType) (clen int) {
	switch {
	case d.bd == 0xc0:
		clen = -1 // to represent nil
	case d.bd == ct.b1:
		clen = int(d.readUint16())
	case d.bd == ct.b2:
		clen = int(d.readUint32())
	case (ct.b0 & d.bd) == ct.b0:
		clen = int(ct.b0 ^ d.bd)
	default:
		d.err("readContainerLen: %s: hex: %x, dec: %d", msgBadDesc, d.bd, d.bd)
	}
	return	
}

func (d *Decoder) chkPtrValue(rv reflect.Value) {
	// We cannot marshal into a non-pointer or a nil pointer 
	// (at least pass a nil interface so we can marshal into it)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		var rvi interface{} = rv
		if rv.IsValid() && rv.CanInterface() {
			rvi = rv.Interface()
		}
		d.err("DecodeValue: Expecting valid pointer to decode into. Got: %v, %T, %v", rv.Kind(), rvi, rvi)
	}
}

func (d *Decoder) readExtLen() (clen int) {
	switch d.bd {
	case 0xc0:
		clen = -1 // to represent nil
	case 0xc9:
		clen = int(d.readUint8())
	case 0xd8:
		clen = int(d.readUint16())
	case 0xd9:
		clen = int(d.readUint32())
	default: 
		switch {
		case d.bd >= 0xc4 && d.bd <= 0xc8:
			clen = int(d.bd & 0x0f)
		case d.bd >= 0xd4 && d.bd <= 0xd7:
			clen = int(d.bd & 0x03)
		default:
			d.err("decoding ext bytes: found unexpected byte: %x", d.bd)
		}
	}
	return
}

func (d *Decoder) err(format string, params ...interface{}) {
	doPanic(msgTagDec, format, params)
}

// DecodeTimeExt decodes a byte stream containing one, two or 3 
// signed integer values into a time.Time, and sets into passed 
// reflectValue.
func DecodeTimeExt(rv reflect.Value, bs []byte) (err error) {
	buf := bytes.NewBuffer(bs)

	d2 := NewDecoder(buf, nil)
	var tt time.Time
	var t0, t1 int64
	var tzOffset int8
	d2.decode(&t0)
	if d2.checkEOF() {
		tt = time.Unix(t0, 0).UTC()
	} else {
		d2.decode(&t1)
		tt = time.Unix(t0, t1).UTC()
		if !d2.checkEOF() {
			d2.decode(&tzOffset)
			tzOffset = tzOffset - 48
			// In stdlib time.Parse, when a date is parsed without a zone name, it use ""
			// var tzSign string
			// tzOffset2 := tzOffset
			// if tzOffset < 0 {
			// 	tzSign = "-"
			// 	tzOffset2 = -tzOffset
			// }
			// locname := fmt.Sprintf("UTC:%s%2d:%2d", tzSign, tzOffset2 / 4, (tzOffset2 % 4) / 4 * 60)				
			tt = tt.In(time.FixedZone("", int(tzOffset) * 15 * 60))
		}
	}

	rv.Set(reflect.ValueOf(tt))
	return
}

// DecodeBinaryExt sets passed byte array AS-IS into the reflect Value.
// Configure this to support the Binary Extension using tag 0.
func DecodeBinaryExt(rv reflect.Value, bs []byte) (err error) {
	rv.SetBytes(bs)
	return
}

// Unmarshal is a convenience function which decodes a stream of bytes into v.
// It delegates to Decoder.Decode.
func Unmarshal(data []byte, v interface{}, o *DecoderOptions) error {
	return NewDecoder(bytes.NewBuffer(data), o).Decode(v)
}

