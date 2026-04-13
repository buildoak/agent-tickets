package main

import (
	"fmt"
	"github.com/buildoak/agent-tickets/frontmatter"
)

func main() {
	doc, err := frontmatter.ParseFile("/Users/otonashi/thinking/pratchett-os/centerpiece/tickets/cards/OPS/ops-001.md")
	if err != nil {
		panic(err)
	}
	doc.Card.Status = frontmatter.StatusOpen
	data, err := doc.Serialize()
	if err != nil {
		panic(err)
	}
	fmt.Println(string(data))
}
