// Package sample holds bound objects whose method signatures reach every shape
// bindgen declares, and every shape it refuses, for bindgen's tests. Nothing
// binds them.
package sample

import (
	"encoding/json/jsontext"
	"time"

	"github.com/bharm16/readmit/desktop/bindgen/internal/sample/words"
)

// Object reaches every shape bindgen declares.
type Object struct{}

func (*Object) Read(workspace string, limit int) Result  { return Result{} }
func (*Object) Write(request Request) (Result, error)    { return Result{}, nil }
func (*Object) Stop() error                              { return nil }
func (*Object) Forget(string, *Item)                     {}
func (*Object) Words() words.Result                      { return words.Result{} }
func (*Object) Choose(_ string, new Mode) map[string]int { return nil }

// Mode is a vocabulary: the union of the constants declared of it, whichever
// way each is written.
type Mode string

const (
	Fast    Mode = "fast"
	Careful      = Mode("care" + "ful")
	Quoted  Mode = Mode(words.Quoted)
	Again   Mode = Fast
	Unset   Mode = ""
)

// The choices a panel offers for a plain string, declared as a block.
const (
	FirstChoice  = "first"
	SecondChoice = words.Quoted
)

// Free declares no constant, so it is any string.
type Free string

// Request reaches every member rule.
type Request struct {
	Name     string         `json:"name"`
	Mode     Mode           `json:"mode"`
	Limit    int            `json:"limit,omitzero"`
	Note     *string        `json:"note"`
	Hint     *string        `json:"hint,omitempty"`
	Tags     []string       `json:"tags"`
	Counts   map[string]int `json:"counts"`
	Raw      jsontext.Value `json:"raw"`
	At       time.Time      `json:"at"`
	Bytes    []byte         `json:"bytes"`
	Any      any            `json:"any"`
	Free     Free           `json:"free"`
	Untagged bool
	Hidden   string `json:"-"`
	hidden   string
	Inline   struct {
		A int `json:"a"`
	} `json:"inline"`
	Items []*Item `json:"items"`
	Promoted
}

// Promoted is embedded untagged, so encoding/json writes its members as the
// embedding struct's own, in its place.
type Promoted struct {
	Origin string `json:"origin"`
	Depth  int    `json:"depth,omitzero"`
}

type Item struct {
	Label string `json:"content-label"`
}

type Result struct {
	State string `json:"state"`
}

// The objects below each reach one shape bindgen refuses.

type Embedding struct{}

func (*Embedding) Get() Embeds { return Embeds{} }

// Embeds embeds a pointer, whose members encoding/json writes only when it is
// not nil.
type Embeds struct{ *Base }

type Base struct {
	ID string `json:"id"`
}

type TaggedEmbedding struct{}

func (*TaggedEmbedding) Get() TaggedEmbeds { return TaggedEmbeds{} }

// TaggedEmbeds embeds a struct under a json tag.
type TaggedEmbeds struct {
	Base `json:"base"`
}

type Shadowing struct{}

func (*Shadowing) Get() Shadows { return Shadows{} }

// Shadows declares a member its embedded struct also declares, and
// encoding/json keeps only one of them.
type Shadows struct {
	Base
	ID string `json:"id"`
}

type SelfMarshaling struct{}

func (*SelfMarshaling) Get() Marshals { return Marshals{} }

type Marshals struct{}

func (Marshals) MarshalJSON() ([]byte, error) { return []byte(`"marshals"`), nil }

type Variadic struct{}

func (*Variadic) Take(names ...string) Result { return Result{} }

type Pair struct{}

func (*Pair) Two() (Result, Result) { return Result{}, Result{} }

type Channel struct{}

func (*Channel) Get() WithChannel { return WithChannel{} }

type WithChannel struct {
	C chan int `json:"c"`
}

type Quoting struct{}

func (*Quoting) Get() QuotedNumber { return QuotedNumber{} }

type QuotedNumber struct {
	N int `json:"n,string"`
}

type Colliding struct{}

func (*Colliding) Mine() Result         { return Result{} }
func (*Colliding) Theirs() words.Result { return words.Result{} }
