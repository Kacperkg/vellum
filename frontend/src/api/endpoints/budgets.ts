import { api } from "../client";
import type {
  Budget,
  CreateBudgetRequest,
  UpdateBudgetRequest,
} from "../types";

const budgetsPath = "/api/budgets";

export async function listBudgets(): Promise<Budget[]> {
  const response = await api.get<Budget[]>(budgetsPath);
  return response.data;
}

export async function createBudget(
  request: CreateBudgetRequest,
): Promise<Budget> {
  const response = await api.post<Budget>(budgetsPath, request);
  return response.data;
}

export async function updateBudget(
  id: string,
  request: UpdateBudgetRequest,
): Promise<Budget> {
  const response = await api.put<Budget>(`${budgetsPath}/${id}`, request);
  return response.data;
}

export async function deleteBudget(id: string): Promise<void> {
  await api.delete(`${budgetsPath}/${id}`);
}
