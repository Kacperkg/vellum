import type { TransactionFrequency } from "./transactions";

export interface Transfer {
  id: string;
  name: string;
  amount: number;
  from_account_id: string;
  to_account_id: string;
  date: string;
  frequency: TransactionFrequency;
}

export type CreateTransferRequest = Omit<Transfer, "id">;
export type UpdateTransferRequest = CreateTransferRequest;
