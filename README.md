# launcher
Wobbling Goblin Launcher

## Building
Create a new file `auth.go` with the following contents:

```go
package main

const (
    accessKeyID = "<access-key>"
    secretAccessKey = "<secret-key>"
)
```

1. `go install github.com/tc-hib/go-winres@latest`
2. `go generate`
3. `go build`

## Running
1. Download `wow335a.zip` and either place the resulting folder in a desirable location or copy the contents of the folder into your 3.3.5a client folder.
2. Run `launcher.exe` to download the client, patch the client, and launch the client.
