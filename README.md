# zoon-go

A Go implementation of [ZOON (Zero Overhead Object Notation)](https://github.com/zoon-format/zoon/blob/main/SPEC.md) - the most token-efficient data format for LLMs.

[![Go Reference](https://pkg.go.dev/badge/github.com/zoon-format/zoon-go.svg)](https://pkg.go.dev/github.com/zoon-format/zoon-go)
[![License](https://img.shields.io/badge/License-MIT-green)](LICENSE)

## Installation

```bash
go get github.com/zoon-format/zoon-go
```

## Usage

### Encoding

```go
package main

import (
    "fmt"
    zoon "github.com/zoon-format/zoon-go"
)

type User struct {
    ID     int    `zoon:"id"`
    Name   string `zoon:"name"`
    Role   string `zoon:"role"`
    Active bool   `zoon:"active"`
}

func main() {
    users := []User{
        {1, "Alice", "Admin", true},
        {2, "Bob", "User", false},
    }

    encoded, _ := zoon.Marshal(users)
    fmt.Println(string(encoded))
    // Output:
    // # id:i+ name:s role=Admin|User active:b
    // Alice Admin 1
    // Bob User 0
}
```

### Decoding

```go
data := `# id:i+ name:s role=Admin|User active:b
Alice Admin 1
Bob User 0`

var users []User
zoon.Unmarshal([]byte(data), &users)
fmt.Printf("%+v\n", users)
// [{ID:1 Name:Alice Role:Admin Active:true} {ID:2 Name:Bob Role:User Active:false}]
```

### Inline Format (Objects)

```go
type Config struct {
    Host string `zoon:"host"`
    Port int    `zoon:"port"`
    SSL  bool   `zoon:"ssl"`
}

cfg := Config{"localhost", 3000, true}
encoded, _ := zoon.Marshal(cfg)
// host=localhost port:3000 ssl:y
```

## API

| Function                              | Description              |
| ------------------------------------- | ------------------------ |
| `Marshal(v any) ([]byte, error)`      | Encode any value to ZOON |
| `Unmarshal(data []byte, v any) error` | Decode ZOON into a value |
| `NewEncoder(w io.Writer) *Encoder`    | Create streaming encoder |
| `NewDecoder(r io.Reader) *Decoder`    | Create streaming decoder |

## Type Mapping

| Go Type           | ZOON Type | Header |
| ----------------- | --------- | ------ |
| `int`             | Integer   | `:i`   |
| `bool`            | Boolean   | `:b`   |
| `string`          | String    | `:s`   |
| `*T` (nil)        | Null      | `~`    |
| Auto-increment ID | Implicit  | `:i+`  |

## License

MIT License - Copyright (c) 2025-PRESENT Carsen Klock
