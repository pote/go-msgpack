
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
	vmsgpack "github.com/vmihailenco/msgpack"
	"launchpad.net/mgo/bson"
	"encoding/json"
	"encoding/gob"
	"testing"
	"bytes"
	"reflect"
	"time"
	"runtime"
	"flag"
	"fmt"
)

// Sample way to run:
// go test -bi -bd=2 -test.bench Msgpack__Encode

var (
	_ = fmt.Printf
	benchBs []byte
	benchTs *TestStruc

	benchDoInitBench = flag.Bool("bi", false, "Run Bench Init")
	//depth of 0 maps to ~400bytes json-encoded string, 1 maps to ~1400 bytes, etc
	benchDepth = flag.Int("bd", 1, "Bench Depth")
	benchInitDebug = flag.Bool("bdbg", false, "Bench Debug")
)

type benchFn func(buf *bytes.Buffer, ts *TestStruc) error

func init() {
	flag.Parse()
	benchTs = newTestStruc(*benchDepth, true)
	//benchBs = make([]byte, 1024 * 4 * d) 
	//initialize benchBs large enough to hold double approx size 
	//(to fit all encodings without expansion)
	approxSize := approxDataSize(reflect.ValueOf(benchTs))	
	bytesLen := 1024 * 4 * (*benchDepth + 1) * (*benchDepth + 1)
	if bytesLen < approxSize {
		bytesLen = approxSize
	}
	// benchBs = make([]byte, approxSize * 2) 
	benchBs = make([]byte, bytesLen)
	if *benchDoInitBench {
		logT(nil, "")
		logT(nil, "..............................................")
		logT(nil, "BENCHMARK INIT: (Depth: %v) %v", *benchDepth, time.Now())
		logT(nil, "TO RUN FULL BENCHMARK comparing MsgPack, V-MsgPack, JSON, BSON, GOB, " + 
			"use \"go test -test.bench .\"")
		logT(nil, "Benchmark: " + 
			"\n\tinit []byte size:                   %d, " +
			"\n\tStruct recursive Depth:             %d, " + 
			"\n\tApproxDeepSize Of benchmark Struct: %d, ", 
			len(benchBs), *benchDepth, approxSize, )
		benchCheck()
		logT(nil, "..............................................")
		if *benchInitDebug {
			fmt.Printf("<<<<====>>>> depth: %v, ts: %#v\n", *benchDepth, benchTs)
		}
	}	
}

func benchCheck() {
	fn := func(name string, encfn benchFn, decfn benchFn) {
		benchBs = benchBs[0:0]
		buf := bytes.NewBuffer(benchBs)
		var err error
		runtime.GC()
		tnow := time.Now()
		if err = encfn(buf, benchTs); err != nil {
			logT(nil, "\t%10s: **** Error encoding benchTs: %v", name, err)
		} 
		encDur := time.Now().Sub(tnow)
		encLen := buf.Len()
		//log("\t%10s: encLen: %v, len: %v, cap: %v\n", name, encLen, len(benchBs), cap(benchBs))
		buf = bytes.NewBuffer(benchBs[0:encLen])
		runtime.GC()
		tnow = time.Now()
		if err = decfn(buf, new(TestStruc)); err != nil {
			logT(nil, "\t%10s: **** Error decoding into new TestStruc: %v", name, err)
		}
		decDur := time.Now().Sub(tnow)
		logT(nil, "\t%10s: Encode Size: %5d, Encode Time: %8v, Decode Time: %8v", name, encLen, encDur, decDur)
	}
	logT(nil, "Benchmark One-Pass Unscientific Marshal Sizes:")
	fn("msgpack", fnMsgpackEncodeFn, fnMsgpackDecodeFn)
	fn("v-msgpack", fnVMsgpackEncodeFn, fnVMsgpackDecodeFn)
	fn("gob", fnGobEncodeFn, fnGobDecodeFn)
	fn("bson", fnBsonEncodeFn, fnBsonDecodeFn)
	fn("json", fnJsonEncodeFn, fnJsonDecodeFn)
}

func fnBenchmarkEncode(b *testing.B, encName string, encfn benchFn) {
	//benchOnce.Do(benchInit)
	runtime.GC()
	// b.ReportAllocs() // GO 1.1+
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchBs = benchBs[0:0]
		buf := bytes.NewBuffer(benchBs)
		if err := encfn(buf, benchTs); err != nil {
			logT(b, "Error encoding benchTs: %s: %v", encName, err)
			b.FailNow()
		}
	}
}

func fnBenchmarkDecode(b *testing.B, encName string, encfn benchFn, decfn benchFn) {
	// logT(b, "CALLING BENCHMARK")
	var err error
	benchBs = benchBs[0:0]
	buf := bytes.NewBuffer(benchBs)
	if err = encfn(buf, benchTs); err != nil {
		logT(b, "Error encoding benchTs: %s: %v", encName, err)
		b.FailNow()
	}
	encLen := buf.Len()
	benchBs = benchBs[0:encLen]
	runtime.GC()
	// b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ts := new(TestStruc)		
		buf = bytes.NewBuffer(benchBs)
		if err = decfn(buf, ts); err != nil {
			logT(b, "Error decoding into new TestStruc: %s: %v", encName, err)
			b.FailNow()
		}
		// TODO: fix this. Currently causes crash in setMapIndex.
		// verifyTsTree(b, ts)
	}
}

func verifyTsTree(b *testing.B, ts *TestStruc) {
	var ts0, ts1m, ts2m, ts1s, ts2s *TestStruc
	ts0 = ts

	if *benchDepth > 0 {
		ts1m, ts1s = verifyCheckAndGet(b, ts0)
	}

	if *benchDepth > 1 {
		ts2m, ts2s = verifyCheckAndGet(b, ts1m)
	}
	for _, tsx := range []*TestStruc{ts0, ts1m, ts2m, ts1s, ts2s} {
		if tsx != nil {
			verifyOneOne(b, tsx)
		}
	}
}

func verifyCheckAndGet(b *testing.B, ts0 *TestStruc) (ts1m *TestStruc, ts1s *TestStruc) {
	// if len(ts1m.Ms) <= 2 {
	// 	logT(b, "Error: ts1m.Ms len should be > 2. Got: %v", len(ts1m.Ms))
	// 	b.FailNow()
	// } 
	if len(ts0.Its) == 0 {
		logT(b, "Error: ts0.Islice len should be > 0. Got: %v", len(ts0.Its))
		b.FailNow()
	}
	ts1m = ts0.Mtsptr["0"]
	ts1s = ts0.Its[0]
	if (ts1m == nil || ts1s == nil) {
		logT(b, "Error: At benchDepth 1, No *TestStruc found")
		b.FailNow()
	}		
	return
}

func verifyOneOne(b *testing.B, ts *TestStruc) {
	if ts.I64slice[2] != int64(3) {
		logT(b, "Error: Decode failed by checking values")
		b.FailNow()
	}
}

func fnVMsgpackEncodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	return vmsgpack.NewEncoder(buf).Encode(ts)
}

func fnVMsgpackDecodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	return vmsgpack.NewDecoder(buf).Decode(ts)
}

func fnMsgpackEncodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	return NewEncoder(buf, testEncOpts).Encode(ts)
}

func fnMsgpackDecodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	dopts := *testDecOpts
	// dopts.RawToString = false
	return NewDecoder(buf, &dopts).Decode(ts)
}

func fnGobEncodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	return gob.NewEncoder(buf).Encode(ts)
}

func fnGobDecodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	return gob.NewDecoder(buf).Decode(ts)
}

func fnJsonEncodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	return json.NewEncoder(buf).Encode(ts)
}

func fnJsonDecodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	return json.NewDecoder(buf).Decode(ts)
}

func fnBsonEncodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	bs, err := bson.Marshal(ts)
	if err == nil {
		buf.Write(bs)
	}
	return err
}

func fnBsonDecodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	return bson.Unmarshal(buf.Bytes(), ts)
}

func Benchmark__Msgpack__Encode(b *testing.B) {
	fnBenchmarkEncode(b, "msgpack", fnMsgpackEncodeFn)
}

func Benchmark__VMsgpack__Encode(b *testing.B) {
	fnBenchmarkEncode(b, "v-msgpack", fnVMsgpackEncodeFn)
}

func Benchmark__Gob______Encode(b *testing.B) {
	fnBenchmarkEncode(b, "gob", fnGobEncodeFn)
}

func Benchmark__Bson_____Encode(b *testing.B) {
	fnBenchmarkEncode(b, "bson", fnBsonEncodeFn)
}

func Benchmark__Json_____Encode(b *testing.B) {
	fnBenchmarkEncode(b, "json", fnJsonEncodeFn)
}

func Benchmark__Msgpack__Decode(b *testing.B) {
	fnBenchmarkDecode(b, "msgpack", fnMsgpackEncodeFn, fnMsgpackDecodeFn)
}

func Benchmark__VMsgpack__Decode(b *testing.B) {
	fnBenchmarkDecode(b, "v-msgpack", fnVMsgpackEncodeFn, fnVMsgpackDecodeFn)
}

func Benchmark__Gob______Decode(b *testing.B) {
	fnBenchmarkDecode(b, "gob", fnGobEncodeFn, fnGobDecodeFn)
}

func Benchmark__Bson_____Decode(b *testing.B) {
	fnBenchmarkDecode(b, "bson", fnBsonEncodeFn, fnBsonDecodeFn)
}

func Benchmark__Json_____Decode(b *testing.B) {
	fnBenchmarkDecode(b, "json", fnJsonEncodeFn, fnJsonDecodeFn)
}

