// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"database/sql"
	"flag"
	"io/ioutil"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/e2e-key-server/appender"
	"github.com/google/e2e-key-server/mutator/entry"
	"github.com/google/e2e-key-server/queue"
	"github.com/google/e2e-key-server/signatures"
	"github.com/google/e2e-key-server/signer"
	"github.com/google/e2e-key-server/tree/sparse/sqlhist"

	"github.com/coreos/etcd/clientv3"
	_ "github.com/mattn/go-sqlite3"
)

var (
	serverDBPath  = flag.String("db", "db", "Database connection string")
	etcdEndpoints = flag.String("etcd", "", "Comma delimited list of etcd endpoints")
	epochDuration = flag.Uint("period", 60, "Seconds between epoch creation")
	mapID         = flag.String("domain", "example.com", "Distinguished name for this key server")
	mapLogURL     = flag.String("maplog", "", "URL of CT server for Signed Map Heads")
	signingKey    = flag.String("key", "", "Path to private key PEM for STH signing")
)

func openDB() *sql.DB {
	db, err := sql.Open("sqlite3", *serverDBPath)
	if err != nil {
		log.Fatalf("sql.Open(): %v", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatalf("db.Ping(): %v", err)
	}
	return db
}

func openEtcd() *clientv3.Client {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   strings.Split(*etcdEndpoints, ","),
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatalf("Failed to connect to etcd: %v", err)
	}
	return cli
}

func openPrivateKey() *signatures.SignatureSigner {
	pem, err := ioutil.ReadFile(*signingKey)
	if err != nil {
		log.Fatalf("Failed to read file %v: %v", *signingKey, err)
	}
	key, _, err := signatures.PrivateKeyFromPEM(pem)
	sig, err := signatures.NewSignatureSigner(key)
	if err != nil {
		log.Fatal("Failed to create signer: %v", err)
	}
	return sig
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	flag.Parse()

	sqldb := openDB()
	defer sqldb.Close()
	etcdCli := openEtcd()
	defer etcdCli.Close()

	queue := queue.New(etcdCli, *mapID)
	tree := sqlhist.New(sqldb, *mapID)
	mutator := entry.New()
	appender := appender.New(sqldb, *mapID, *mapLogURL)

	signer := signer.New(queue, tree, mutator, appender, openPrivateKey())
	go signer.StartSequencing()
	go signer.StartSigning(time.Duration(*epochDuration) * time.Second)

	log.Printf("Signer started.")

	var wg sync.WaitGroup
	wg.Add(1)
	wg.Wait()
}
