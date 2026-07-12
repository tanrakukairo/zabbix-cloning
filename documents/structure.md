# Zabbix Cloning structure

## Directory layout

```text
zabbix-cloning/
├── go-lang/                 # Primary Go implementation (Zabbix 6.0+)
│   ├── cmd/
│   │   ├── zc/              # Clone CLI
│   │   ├── view/            # Datastore viewer
│   │   └── ...              # Independent maintenance tools
│   ├── internal/
│   │   ├── clone/           # Master/replica lifecycle and transformations
│   │   ├── config/          # CLI, environment and JSON configuration
│   │   ├── model/           # Shared datastore model
│   │   ├── store/           # File, Redis and DynamoDB backends
│   │   ├── zabbix/          # JSON-RPC client
│   │   └── view/            # Datastore display logic
│   ├── go.mod
│   └── go.sum
├── prototype/               # Former Python implementation
│   ├── zc.py
│   ├── view.py
│   ├── tools/
│   └── zc/
├── documents/
├── images/
├── test_config/
├── readme.md
└── readme_en.md
```

## Go ownership boundaries

- `config` owns parsing and normalization. Other packages consume `Config` and
  do not inspect command-line arguments directly.
- `zabbix` owns transport, authentication, JSON-RPC errors and version parsing.
- `store` owns persistent encoding. All backends expose the same `Store`
  interface and share the `model.Dataset` contract.
- `clone` owns clone ordering and Zabbix-specific data transformations.
- `cmd` packages contain only process setup and exit-code handling.

## Clone flow

```text
CLI/config
  -> Zabbix connection and version detection
  -> first process / local state collection
  -> master: export -> normalize -> store -> version macro
  -> replica: load -> globals -> API PRE -> configuration
              -> hosts/interfaces -> API POST/ACCOUNT
              -> authentication/media -> version macro
```

## Compatibility boundary

The Go implementation supports Zabbix 6.0 and newer. The Python prototype is
kept for Zabbix 4.x and 5.x because those versions require direct PostgreSQL or
MySQL operations that are intentionally outside the Go implementation.

File, Redis and DynamoDB payloads remain bidirectionally compatible. Existing
Python-generated versions can therefore be consumed by the Go CLI without a
migration step.
