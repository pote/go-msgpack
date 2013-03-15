MsgPack library for Go.
=======================

Implements: http://wiki.msgpack.org/display/MSGPACK/Format+specification  
API Docs:   http://godoc.org/github.com/ugorji/go-msgpack

To install:
>  go get github.com/ugorji/go-msgpack

This library provides features serialization and deserialization support for msgpack,
with a comparable feature set to Go's standard library encoding packages (json, xml, gob, etc). 

Features

  - Standard Marshal/Unmarshal interface.
  - Standard field renaming via tags
  - Encoding from any value 
    (struct, slice, map, primitives, pointers, interface{}, etc)
  - Decoding into pointer to any non-nil typed value 
    (struct, slice, map, int, float32, bool, string, reflect.Value, etc)
  - Decoding into a pointer to a nil interface{} 
  - Handles binaries and time via provided extensions 
  - Supports Basic and new in-progress Application Profile with extension support.
  - Options to configure how to handle raw bytes in stream 
    (as string or []byte)
  - Options to configure what type to use when decoding a list or map 
    into a nil interface{}
  - Provides a Server and Client Codec so msgpack can be used as 
    communication protocol for net/rpc.
    - Also includes an option for msgpack-rpc: 
      http://wiki.msgpack.org/display/MSGPACK/RPC+specification

Extension Support
-----------------

Users can register a function to handle the encoding or decoding of
their custom types. There is no restriction on what the custom type can
be. Even though it adds a performance cost, we believe users should not
have to retrofit their code to accomodate the library.

Extensions can be any type: pointers, structs, custom types off
arrays/slices, strings, etc.

Some examples:
> type BisSet   []int  
  type BitSet64 uint64  
  type UUID     string  
  type MyTimeWithUnexportedFields struct { a int; b bool; c []int; }  
  type GifImage struct { ... }  

Typically, MyTimeWithUnexportedFields is encoded as an empty map because
it has no exported fields, while UUID will be encoded as a string,
etc. However, with extension support, you can encode any of these
however you like.

We provide implementations of these functions where the spec has defined
an inter-operable format e.g. Binary and time.Time. Library users will
have to explicitly configure these as seen in the usage below.

RPC
-----

An RPC Client and Server Codec is implemented, so that msgpack can be used
with the standard net/rpc package. It supports both a basic net/rpc serialization,
and the custom format defined at http://wiki.msgpack.org/display/MSGPACK/RPC+specification.

Usage
-----
<pre>
  // create and configure options
  dopts = msgpack.NewDecoderOptions()
  dopts.MapType = reflect.TypeOf(map[string]interface{}(nil))
  dopts.RawToString = true // only used if binary extension is not configured.
  
  eopts = msgpack.NewEncoderOptions()
  
  // configure extensions, to enable Binary and Time support for tags 1 and 2
  // Note that configuring Binary Extensions here ensures Raw is string and binary is ext.
  dopts.AddExt(reflect.TypeOf([]byte(nil)), 0, msgpack.DecodeBinaryExt)
  dopts.AddExt(reflect.TypeOf(time.Time{}), 1, msgpack.DecodeTimeExt)

  eopts.AddExt(reflect.TypeOf([]byte(nil)), 0, msgpack.EncodeBinaryExt)
  eopts.AddExt(reflect.TypeOf(time.Time{}), 1, msgpack.EncodeTimeExt)
  
  // create decoder
  dec = msgpack.NewDecoder(r, dopts)
  err = dec.Decode(&v) 
  
  // create encoder
  enc = msgpack.NewEncoder(w, eopts)
  err = enc.Encode(v) 
  
  //convenience functions dealing with []byte
  data, err = msgpack.Marshal(v, eopts) 
  err = msgpack.Unmarshal(data, &v, dopts)
  
  //RPC Server
  conn, err := listener.Accept()
  rpcCodec := msgpack.GoRpc.ServerCodec(conn, nil, nil)
  rpc.ServeCodec(rpcCodec)

  //RPC Communication (client side)
  conn, err = net.Dial("tcp", "localhost:5555")  
  rpcCodec := msgpack.GoRpc.ClientCodec(conn, nil, nil)  
  client := rpc.NewClientWithCodec(rpcCodec)  
 
</pre>
