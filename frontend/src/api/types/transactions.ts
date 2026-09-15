export type TransactionFrequency =
  "one-off" | "daily" | "weekly" | "monthly" | "yearly";

export type TransactionType = "income" | "expense";

export interface Transaction {
  id: string;
  name: string;
  amount: number;
  account_id: string;
  category_id: string;
  date: string;
  frequency: TransactionFrequency;
  type: TransactionType;
}

export interface CreateTransactionRequest {
  name: string;
  amount: number;
  account_id: string;
  category_id: string;
  date: string;
  frequency: TransactionFrequency;
  type: TransactionType;
}

export type UpdateTransactionRequest = CreateTransactionRequest;
