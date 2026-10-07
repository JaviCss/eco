package main

import (
	"bufio"
	"context"
	"os"

	"github.com/JaviCss/eco/mcpdoor"
	"github.com/JaviCss/eco/store"
)

func mcpVerb(args []string) {
	v := parseFlags("mcp", args)
	s := openStore(v.userDB, v.projectDB, "mcp", store.ProfileAgent)
	defer s.Close()
	server := mcpdoor.New(s)
	if err := server.Serve(context.Background(), bufio.NewReader(os.Stdin), os.Stdout); err != nil {
		fail("mcp", err)
	}
}