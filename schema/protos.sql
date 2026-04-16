CREATE TABLE packages (
    id             INTEGER PRIMARY KEY,
    repo           TEXT NOT NULL,
    proto_package  TEXT NOT NULL,
    file_count     INTEGER NOT NULL,
    descriptor_set BLOB NOT NULL
);
CREATE INDEX packages_name ON packages(proto_package);

CREATE TABLE symbols (
    id          INTEGER PRIMARY KEY,
    package_id  INTEGER NOT NULL REFERENCES packages(id),
    kind        TEXT NOT NULL, -- message | service | method | enum
    name        TEXT NOT NULL,
    fqn         TEXT NOT NULL,
    file_path   TEXT NOT NULL,
    line        INTEGER,
    descriptor  BLOB NOT NULL,
    input_fqn   TEXT, -- methods only
    output_fqn  TEXT  -- methods only
);
CREATE INDEX symbols_fqn  ON symbols(fqn);
CREATE INDEX symbols_name ON symbols(name);
CREATE INDEX symbols_kind ON symbols(kind);
