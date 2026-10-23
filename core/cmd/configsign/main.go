package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	core "github.com/sonjiwu2/youtube-android-zapret/core"
	"os"
	"path/filepath"
)

func main() {
	key := flag.String("key", "../signing/private-config.key", "private key path (ignored by git)")
	pub := flag.String("public", "../app/src/main/assets/config-public.txt", "public key path")
	generate := flag.Bool("generate", false, "generate a new key pair")
	file := flag.String("file", "", "exact JSON file to sign")
	flag.Parse()
	fail := func(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
	if *generate {
		if _, err := os.Stat(*key); !os.IsNotExist(err) {
			fail(fmt.Errorf("refusing to replace existing key"))
		}
		p, s, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fail(err)
		}
		if err = os.MkdirAll(filepath.Dir(*key), 0700); err != nil {
			fail(err)
		}
		if err = os.WriteFile(*key, []byte(base64.StdEncoding.EncodeToString(s)), 0600); err != nil {
			fail(err)
		}
		if err = os.WriteFile(*pub, []byte(base64.StdEncoding.EncodeToString(p)+"\n"), 0644); err != nil {
			fail(err)
		}
		return
	}
	data, err := os.ReadFile(*file)
	if err != nil {
		fail(err)
	}
	if _, err = core.ParseConfig(data); err != nil {
		fail(err)
	}
	raw, err := os.ReadFile(*key)
	if err != nil {
		fail(err)
	}
	s, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil || len(s) != ed25519.PrivateKeySize {
		fail(fmt.Errorf("invalid private key"))
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(s, data))
	if err = os.WriteFile(*file+".sig", []byte(sig+"\n"), 0644); err != nil {
		fail(err)
	}
}
