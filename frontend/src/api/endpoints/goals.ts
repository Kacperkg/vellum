import { api } from "../client";
import type { CreateGoalRequest, Goal, UpdateGoalRequest } from "../types";

const goalsPath = "/api/goals";

export async function listGoals(): Promise<Goal[]> {
  const response = await api.get<Goal[]>(goalsPath);
  return response.data;
}

export async function createGoal(request: CreateGoalRequest): Promise<Goal> {
  const response = await api.post<Goal>(goalsPath, request);
  return response.data;
}

export async function updateGoal(
  id: string,
  request: UpdateGoalRequest,
): Promise<Goal> {
  const response = await api.put<Goal>(`${goalsPath}/${id}`, request);
  return response.data;
}

export async function deleteGoal(id: string): Promise<void> {
  await api.delete(`${goalsPath}/${id}`);
}
