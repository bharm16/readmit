export interface ReceivedEngine {
  address: string;
  setMode(mode: "defective" | "fixed"): void;
  received(): string[];
  outputs(): string[];
  reset(): void;
  close(): Promise<void>;
}
export function startReceivedEngine(
  captureAddress: string,
): Promise<ReceivedEngine>;
