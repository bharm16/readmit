export interface DatabaseEngine {
  version: string;
  address: string;
  databaseAddress: string;
  setMode(mode: "wrong" | "fixed"): void;
  reset(): void;
  received(): number;
  frames(): string[];
  errors(): string[];
  close(): Promise<void>;
}
export function startDatabaseEngine(options: {
  postgresBin: string;
  certificate: string;
  key: string;
}): Promise<DatabaseEngine>;
