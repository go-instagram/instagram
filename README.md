<p align="center"><img src="https://raw.githubusercontent.com/go-instagram/brand/main/social/go-instagram.png" alt="go-instagram/instagram" width="720"></p>

# instagram

[![CI](https://github.com/go-instagram/instagram/actions/workflows/ci.yml/badge.svg)](https://github.com/go-instagram/instagram/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/go-instagram/instagram.svg)](https://pkg.go.dev/github.com/go-instagram/instagram)
[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](LICENSE)

A pure-Go, dependency-free, **best-effort** read client for public Instagram
content served through Instagram's web JSON endpoints.

- **CGO-free** (`CGO_ENABLED=0`), Go 1.26.4 floor.
- **Zero third-party dependencies** — standard library only.
- Small, stable Go API that hides an inherently unstable transport.

## ⚠️ Fragility & Terms-of-Service caveat

**This library is fragile by nature. Read this before depending on it.**

Instagram does **not** offer these endpoints as a stable, documented public API.
They are the internal endpoints its own website calls, and they change, rate-limit,
and lock without notice. In practice this means:

- Unauthenticated requests are frequently rejected. You will often need a valid
  logged-in **`sessionid`** cookie (see `WithSessionID`) for reads to succeed.
- Any request can start returning **401 / 403 / 429** at any time when Instagram
  decides to block you. This library surfaces those as clear errors that mention
  the status — they indicate blocking/fragility, **not** a bug in the code.
- The response shape can change at any time, which will surface as decode errors.
- Automated scraping of Instagram may violate Instagram's Terms of Service and/or
  local law. **You are responsible for how you use this library.** Use it only for
  content and in ways you are authorized to access.

The Go API is kept deliberately small and stable so your code can survive the
churn underneath. The underlying transport may break at any time regardless.

## Install

```sh
go get github.com/go-instagram/instagram
```

## Usage

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/go-instagram/instagram"
)

func main() {
	// A sessionid cookie is usually required for reads to succeed.
	c := instagram.New(
		instagram.WithSessionID("your-sessionid-cookie"),
	)

	prof, err := c.UserProfile(context.Background(), "instagram")
	if err != nil {
		log.Fatal(err) // 401/403/429 here means Instagram is blocking the request.
	}

	fmt.Printf("%s (%s) — %d followers\n", prof.FullName, prof.Username, prof.Followers)
	for _, p := range prof.Posts {
		fmt.Printf("- %s  %d likes  %s\n", p.Permalink, p.Likes, p.Caption)
	}
}
```

### Options

| Option | Purpose |
| ------ | ------- |
| `WithHTTPClient(*http.Client)` | Custom HTTP client (timeouts, proxy, transport). |
| `WithBaseURL(string)` | Override the request origin (defaults to `https://www.instagram.com`). |
| `WithUserAgent(string)` | Set the `User-Agent` header. |
| `WithSessionID(string)` | Send a `sessionid` cookie for authenticated reads. |

## License

BSD-3-Clause. See [LICENSE](LICENSE). Copyright the go-instagram/instagram authors.
