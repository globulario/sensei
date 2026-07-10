// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// emitProtoJSON writes a gRPC response as canonical proto JSON and returns the
// process exit code.
//
// It exists because encoding/json on a generated proto renders enum fields as
// their integer values (risk_class: 4), forcing every agent/tool consumer to
// reverse-map the numbers. protojson renders enums as their string names
// (risk_class: "SECURITY_RISK"), which is the stable, self-describing contract
// a machine reader needs. UseProtoNames keeps the snake_case field names from
// the .proto so only the enum encoding changes; zero fields stay omitted.
func emitProtoJSON(m proto.Message) int {
	b, err := protojson.MarshalOptions{Multiline: true, Indent: "  ", UseProtoNames: true}.Marshal(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg: encode json: %v\n", err)
		return 1
	}
	fmt.Println(string(b))
	return 0
}
