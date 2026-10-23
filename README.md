# ubimi - Ubuntu Image Indexer

This is a simple `go` program that keeps and serves an in-memory index of a
RadosGW provided swift storage. This is essentially a replacement for Apache's
autoindex feature, against a more modern Object Storage.

## Architecture

This program is not intended to serve the bytes stored on that object storage,
but rather simply points to them.
Intended deployement is as follow:
```
        ------------ HAProxy -----------
        |                              |
frontend filtering /images/      default frontend
        |                              |
        V                              V
    RadosGW <------ indexes -------- ubimi
```

The architecture of ubimi keeps the swift calls abstracted behind a generic
"storage" interface, allowing easy swapping of the Object Storage backend.
Similarly, a separate routing interface allows easy addition of any HTTP routes
based on both the hostname and the URI to provide an easy to maintain redirect
farm, facilitating the transition from previous legacy storage solution.

## Local development

For local development, the [`saio`](https://hub.docker.com/r/openstackswift/saio)
Docker images can provide a dead simple throw-away `swift` interface.
