# Game data manifests

Each supported game publishes an immutable manifest containing its game key, data version, logical hash and
SQLite file hash. The platform imports only artifacts whose hashes match the manifest.
