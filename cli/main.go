package main

import (
	"encoding/json"
	"fmt"

	"github.com/happenslol/equ"
)

func main() {
	testString := `(name.first[eq:"foo"|eq:"bar"|eq:"baz"]|email[ct:"foo",ct:"bar"]),age[(gt:4.5,lt:-10)|eq:15]`

	result, err := equ.Parse(testString)
  if err != nil {
    panic(err)
  }

	m := equ.ToMap(result)

	b, _ := json.MarshalIndent(m, "", "  ")
	fmt.Print(string(b) + "\n")
}
