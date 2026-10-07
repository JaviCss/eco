package main

import (
	"bufio"
	"context"
	"os"
	"strings"

	"github.com/JaviCss/eco/mcpdoor"
	"github.com/JaviCss/eco/store"
)

func mcpVerb(args []string) {
	v := parseFlags("mcp", args)
	if strings.TrimSpace(v.userDB) == "" || strings.TrimSpace(v.projectDB) == "" {
		refuse("mcp", "--user-db and --project-db are required")
	}
	s := openStore(v.userDB, v.projectDB, "mcp", store.ProfileAgent)
	defer s.Close()
	server := mcpdoor.New(s)
	if err := server.Serve(context.Background(), bufio.NewReader(os.Stdin), os.Stdout); err != nil {
		fail("mcp", err)
	}
}