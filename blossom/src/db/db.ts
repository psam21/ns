import Database from "better-sqlite3";
import { BlossomSQLite } from "blossom-server-sdk/metadata/sqlite";
import { config } from "../config.js";
import { mkdirp } from "mkdirp";
import { dirname } from "path";

await mkdirp(dirname(config.databasePath));

export const db = new Database(config.databasePath);
export const blobDB = new BlossomSQLite(db);

db.prepare(
  `CREATE TABLE IF NOT EXISTS accessed (
		blob TEXT(64) PRIMARY KEY,
		timestamp INTEGER NOT NULL
	)`,
).run();

db.prepare("CREATE INDEX IF NOT EXISTS accessed_timestamp ON accessed (timestamp)").run();

// Add an index on blobs.type so pruneStorage's WHERE blobs.type LIKE ?
// does not full-scan the table (issue #95). The BlossomSQLite helper
// creates the blobs table; we add the index immediately after.
db.prepare("CREATE INDEX IF NOT EXISTS blobs_type ON blobs(type)").run();

export default db;
