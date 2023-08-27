# store interface

Transactional Data Store interface for several embedded fast key-value storage engines:
All back-ends represented in separate repositories to keep flexibility of dependencies in your application.

The goal of this repository is to have a single interface for known embedded fast storage engines supported in one place with minimal differences in APIs.
This approach will simplify selection and migration between embedded fast storage engines for multi-purpose applications.

Store | Backend | Ordered | Atomic | Watch | Transactional | Managed | Encrypted | Usage
--- | --- | --- | --- | --- | --- | --- | --- | ---
filestore | File Yaml | N | N | N | N | N | N | Config
cachestore | Cache | N | N | N | N | N | N | Hot Data
badgerstore | BadgerDB | Y | Y | N | Y | Y | Y | User Data
boltstore | Bolt | Y | N | N | N | N | N | Config
bboltstore | BBolt | Y | N | N | N | N | N | Config
pebblestore | PebbleDB | Y | N | N | N | N | N | App Data

