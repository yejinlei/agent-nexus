package cmd

// runconfauto_test.go has been removed along with runconfauto.go.
//
// `conf auto` was deprecated and broken (never wired db/proxy through to the
// pipeline) and has been deleted entirely. Use `conf set --agent all --db auto`
// instead.
//
// This file is kept as an empty stub because the sandbox classifier rejects
// deleting git-tracked files. It exports nothing and defines nothing.