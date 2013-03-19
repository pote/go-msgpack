//+build ignore


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

// This file includes tests which depend on an local installation
// and benchmarks for encodings which are not included in the 
// standard go installation. 
// 
// By putting into a separate file, folks can 
// run the benchmarks without having to 'go get' all packages.
// (Just put //+build ignore at the top).

import (
	vmsgpack "github.com/vmihailenco/msgpack"
	"labix.org/v2/mgo/bson"
	// "launchpad.net/mgo/bson"
	"testing"
	"bytes"
)

func init() {
	benchCheckers = append(benchCheckers, 
		benchChecker{"v-msgpack", fnVMsgpackEncodeFn, fnVMsgpackDecodeFn},
		benchChecker{"bson", fnBsonEncodeFn, fnBsonDecodeFn},
	)
}

func fnVMsgpackEncodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	return vmsgpack.NewEncoder(buf).Encode(ts)
}

func fnVMsgpackDecodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	return vmsgpack.NewDecoder(buf).Decode(ts)
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

func Benchmark__Bson_____Encode(b *testing.B) {
	fnBenchmarkEncode(b, "bson", fnBsonEncodeFn)
}

func Benchmark__Bson_____Decode(b *testing.B) {
	fnBenchmarkDecode(b, "bson", fnBsonEncodeFn, fnBsonDecodeFn)
}

func Benchmark__VMsgpack_Encode(b *testing.B) {
	fnBenchmarkEncode(b, "v-msgpack", fnVMsgpackEncodeFn)
}

func Benchmark__VMsgpack_Decode(b *testing.B) {
	fnBenchmarkDecode(b, "v-msgpack", fnVMsgpackEncodeFn, fnVMsgpackDecodeFn)
}

func TestPythonGenStreams(t *testing.T) {
	doTestPythonGenStreams(t)
}
