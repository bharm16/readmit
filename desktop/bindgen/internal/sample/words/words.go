// Package words declares a constant another package's vocabulary refers to,
// and a type whose name the sample package also uses, for bindgen's tests.
package words

// Quoted is a word the sample vocabulary takes by reference.
const Quoted = "quoted"

// Result shares its name with the sample package's own Result.
type Result struct {
	Word string `json:"word"`
}
