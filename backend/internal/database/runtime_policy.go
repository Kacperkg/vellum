package database

import _ "embed"

// The same read-only query can be run independently by an operator or trusted
// controller using the verified source. Its result is one Boolean value.
//
//go:embed runtime_policy.sql
var runtimePolicySQL string

//go:embed runtime_grants.sql
var runtimeGrantsSQL string
