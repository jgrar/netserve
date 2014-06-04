package main

import (
	"fmt"
	"io"
	"bufio"
	"encoding/json"
	"github.com/jgrar/ircmessage"
)

var (
	Filters = map[string]func (io.Reader) Filter {
		"irctojson": NewIrcToJson,
		"jsontoirc": NewJsonToIrc,
		"raw": NewRaw,
	}
)

type Filter interface{
	Scan() bool
	Err() error
	Bytes() []byte
	Text() string
}

type NewFilter func (io.Reader) Filter

func (f *NewFilter) Set (key string) error {
	var ok bool
	if *f, ok = Filters[key]; !ok {
		return fmt.Errorf("unknown filter: %s", key)
	}
	return nil
}

func (f *NewFilter) String () string {
	return "raw"
}

func NewRaw (r io.Reader) Filter {
	return bufio.NewScanner(r)
}

func NewIrcToJson (r io.Reader) Filter {
	s := bufio.NewScanner(r)
	s.Split(ircmessage.RawToJsonIRC)
	return s
}

type JsonToIrc struct{
	token []byte
	err error
	dec *json.Decoder
}

func NewJsonToIrc (r io.Reader) Filter {
	return &JsonToIrc{
		dec: json.NewDecoder(r),
	}
}

func (f *JsonToIrc) Scan () bool {
	var msg ircmessage.IRCMessage

	f.err = f.dec.Decode(&msg) // don't use decoder, underlying buffer may change

	if f.err != nil {
		return false
	}

	f.token, f.err = msg.Marshal()

	if f.err != nil {
		return false
	}

	f.token = append(f.token, "\r\n"...)

	return true
}

func (f *JsonToIrc) Err () error {
	if f.err == io.EOF {
		return nil
	}
	return f.err
}

func (f *JsonToIrc) Bytes () []byte {
	return f.token
}

func (f *JsonToIrc) Text () string {
	return string(f.token)
}

