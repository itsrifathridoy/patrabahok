import { drizzle } from "drizzle-orm/node-postgres";
import { Pool } from "pg";
import * as schema from "./schema";

declare global {
  var __patrabahokMailPool: Pool | undefined;
}

const pool =
  global.__patrabahokMailPool ??
  new Pool({
    connectionString: process.env.DATABASE_URL,
    max: 10,
  });

if (process.env.NODE_ENV !== "production") {
  global.__patrabahokMailPool = pool;
}

export const db = drizzle(pool, { schema });
