import { api } from "../client";
import type {
  CreateTransactionRequest,
  Transaction,
  UpdateTransactionRequest,
} from "../types";

const transactionsPath = "/api/transactions";

export async function listTransactions(): Promise<Transaction[]> {
  const response = await api.get<Transaction[]>(transactionsPath);
  return response.data;
}

export async function createTransaction(
  request: CreateTransactionRequest,
): Promise<Transaction> {
  const response = await api.post<Transaction>(transactionsPath, request);
  return response.data;
}

export async function updateTransaction(
  id: string,
  request: UpdateTransactionRequest,
): Promise<Transaction> {
  const response = await api.put<Transaction>(
    `${transactionsPath}/${id}`,
    request,
  );
  return response.data;
}

export async function deleteTransaction(id: string): Promise<void> {
  await api.delete(`${transactionsPath}/${id}`);
}
