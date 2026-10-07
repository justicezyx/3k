// Separate module so this repo's `go build ./...` and `go test ./...`
// do not compile the historical repro tests. run.sh copies *_test.go
// into a NascentCore/3k checkout at 17b19cf and tests them there.
// designmodel/ has its own module and is run in place.
module sxwl/3k/tests/3k-repro

go 1.21
