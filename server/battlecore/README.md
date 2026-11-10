# Shared battle core

This package runs individual battles. It owns combat state, random number generation,
skill effects, turn phases, and battle results. Room management, networking,
accounts, and persistence belong to the calling service.

Load definitions with `battlecontent.LoadDefinitions`, validate them with
`battlecore.NewCatalog`, then pass the catalog, configuration, and participants to
`NewBattleEngine`. Treat the shared catalog as read-only once battles can access it.
The caller serializes operations for each battle and handles deadlines and rewards.

`battlemodel` defines shared values. `battlecontent` parses combat data, and
`battlecodec` formats client messages. Core tests can run locally with
`go test ./battlecore` from the `server` directory.

Card, enemy, enemy-level and gauge readers consume the unified table layout.
Room rewards use one per-wave drop plan; settlement consumes the drops actually
released by the engine. Account state and reward persistence stay in the service.
