/*
 * Copyright 2019 Dgraph Labs, Inc. and Contributors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/dgraph-io/dgo"
	"github.com/dgraph-io/dgo/protos/api"
	"github.com/dgraph-io/dgraph/z"
	"github.com/stretchr/testify/require"
)

var (
	statePattern = `"snapshotTs":"([0-9]*)"`
)

func TestSnapshot(t *testing.T) {
	snapshotTs := uint64(0)

	dg1 := z.DgraphClient("localhost:9180")
	dg1.Alter(context.Background(), &api.Operation{
		DropOp: api.Operation_ALL,
	})
	dg1.Alter(context.Background(), &api.Operation{
		Schema: "value: int .",
	})

	for i := 1; i <= 10; i++ {
		log.Println(i)
		dg1.NewTxn().Mutate(context.Background(),
			&api.Mutation{
				SetNquads: []byte(fmt.Sprintf(`_:node <value> "%d" .`, i)),
				CommitNow: true,
			})
	}
	doQuery(t, dg1, 55)

	err := z.DockerStop("alpha2")
	require.NoError(t, err)

	for i := 11; i <= 600; i++ {
		log.Println(i)
		dg1.NewTxn().Mutate(context.Background(),
			&api.Mutation{
				SetNquads: []byte(fmt.Sprintf(`_:node <value> "%d" .`, i)),
				CommitNow: true,
			})
	}
	snapshotTs = waitForSnapshot(t, snapshotTs)

	err = z.DockerStart("alpha2")
	require.NoError(t, err)

	dg2 := z.DgraphClient("localhost:9182")
	doQuery(t, dg2, 180300)

	err = z.DockerStop("alpha2")
	require.NoError(t, err)

	for i := 601; i <= 1200; i++ {
		log.Println(i)
		dg1.NewTxn().Mutate(context.Background(),
			&api.Mutation{
				SetNquads: []byte(fmt.Sprintf(`_:node <value> "%d" .`, i)),
				CommitNow: true,
			})
	}
	snapshotTs = waitForSnapshot(t, snapshotTs)

	err = z.DockerStart("alpha2")
	require.NoError(t, err)

	dg2 = z.DgraphClient("localhost:9182")
	doQuery(t, dg2, 720600)
}

func doQuery(t *testing.T, dg *dgo.Dgraph, total int) {
	q := `
	{
		var(func: has(value)) {
			v as value
		}

		total() {
			sum(val(v))
		}
	}`
	resp, err := z.RetryQuery(dg, q)
	require.NoError(t, err)
	z.CompareJSON(t, fmt.Sprintf(`{"total": [{"sum(val(v))": %d}]}`, total), string(resp.Json))
}

func waitForSnapshot(t *testing.T, prevSnapTs uint64) uint64 {
	for {
		res, err := http.Get("http://localhost:6180/state")
		require.NoError(t, err)
		body, err := ioutil.ReadAll(res.Body)
		res.Body.Close()
		require.NoError(t, err)

		regex, err := regexp.Compile(statePattern)
		require.NoError(t, err)

		matches := regex.FindAllStringSubmatch(string(body), 1)
		if len(matches) == 0 {
			time.Sleep(time.Second)
			continue
		}

		snapshotTs, err := strconv.ParseUint(matches[0][1], 10, 64)
		log.Printf("last snapshot %d. snapshot from zero %d\n", prevSnapTs, snapshotTs)
		require.NoError(t, err)
		if snapshotTs > prevSnapTs {
			return snapshotTs
		}

		time.Sleep(time.Second)
	}
}
