/*
MsgPack library for Go.

Implements:
  http://wiki.msgpack.org/display/MSGPACK/Format+specification

It provides features similar to encoding packages in the standard library 
(ie json, xml, gob, etc).

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
    Users can pass custom functions to handle the encode/decode of custom types.
      Examples:
        type Bitset []int
        type UUID []byte
        type MyStruct { // no exported fields }
        type MyID int64
  - Options to configure how to handle raw bytes in stream 
    (as string or []byte)
  - Options to configure what type to use when decoding a list or map 
    into a nil interface{}
  - Provides a Server and Client Codec so msgpack can be used as 
    communication protocol for net/rpc.
    Also includes an option for msgpack-rpc: 
    http://wiki.msgpack.org/display/MSGPACK/RPC+specification

Extension Support

Users can register a function to handle the encoding or decoding of
their custom types.  There is no restriction on what the custom type can
be. Even though it adds a performance cost, we believe users should not
have to retrofit their code to accomodate the library.

Extensions can be any type: pointers, structs, custom types off
arrays/slices, strings, etc.

Some examples:
  type BisSet   []int
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

Usage

  // create and configure options
  dopts = msgpack.NewDecoderOptions()
  dopts.MapType = reflect.TypeOf(map[string]interface{}(nil))

  eopts = msgpack.NewEncoderOptions()
  
  // configure extensions, to enable Binary and Time support for tags 1 and 2
  dopts.AddExt(reflect.TypeOf([]byte(nil)), 0,msgpack. DecodeBinaryExt)
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
 
*/
package msgpack

/*

CODE ORGANIZATION

Code here is organized as follows:
Exported methods are not called internally. They are just facades.
  Marshal calls Encode 
  Encode calls EncodeValue 
  EncodeValue calls encodeValue 

encodeValue and all other unexported functions use panics (not errors)
   and may call other unexported functions (which use panics).

Same idea/organization is used for decode.go.

HANDLING ERRORS

Any underlying errors are returned as is. Consequently, you may see
Some network errors, etc passed back up.
We support this by panic'ing with the error if we did not create it.

EXTENSION SUPPORT

We support registering an ext func for any type. This allows folks 
code with their custom types (not structs) and register/handling
its encoding or decoding themselves.

For example:
  type BisSet *[]uint8
  type UUID string
  type MyTimeWithUnexportedFields struct { ... }

To support all these, we need to check if they are registered at the 
top of the Encode and Decodes.

TODO: Optimization:
  - Keep a list of all reflect.Type registered, and use better algorithms
    to quickly check.
*/
