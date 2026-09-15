import { api } from "../client";
import type {
  Account,
  CreateAccountRequest,
  UpdateAccountRequest,
} from "../types/index";

const accountsPath = "/api/accounts";

export async function listAccounts(): Promise<Account[]> {
  const response = await api.get<Account[]>(accountsPath);
  return response.data;
}

export async function getAccount(id: string): Promise<Account> {
  const response = await api.get<Account>(`${accountsPath}/${id}`);
  return response.data;
}

export async function createAccount(
  request: CreateAccountRequest,
): Promise<Account> {
  const response = await api.post<Account>(accountsPath, request);
  return response.data;
}

export async function updateAccount(
  id: string,
  request: UpdateAccountRequest,
): Promise<Account> {
  const response = await api.put<Account>(`${accountsPath}/${id}`, request);
  return response.data;
}

export async function deleteAccount(id: string): Promise<void> {
  await api.delete(`${accountsPath}/${id}`);
}
