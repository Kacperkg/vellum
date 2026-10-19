import { api } from "../client";
import type {
  CreateTransferRequest,
  Transfer,
  UpdateTransferRequest,
} from "../types";

const transfersPath = "/api/transfers";

export async function listTransfers(): Promise<Transfer[]> {
  const response = await api.get<Transfer[]>(transfersPath);
  return response.data;
}

export async function createTransfer(
  request: CreateTransferRequest,
): Promise<Transfer> {
  const response = await api.post<Transfer>(transfersPath, request);
  return response.data;
}

export async function updateTransfer(
  id: string,
  request: UpdateTransferRequest,
): Promise<Transfer> {
  const response = await api.put<Transfer>(`${transfersPath}/${id}`, request);
  return response.data;
}

export async function deleteTransfer(id: string): Promise<void> {
  await api.delete(`${transfersPath}/${id}`);
}
