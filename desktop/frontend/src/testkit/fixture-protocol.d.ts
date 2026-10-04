export interface FixtureProtocol {
  url: string;
  authorities: string;
  encodedAuthorities: string;
  created(): number;
  deleted(): number;
  close(): Promise<void>;
}
export function startFixtureProtocol(options: {
  folder: string;
  project: string;
  reset: () => void;
}): Promise<FixtureProtocol>;
