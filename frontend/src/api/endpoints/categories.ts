import { api } from "../client";
import type { Category } from "../types";

export async function listCategories(): Promise<Category[]> {
  const response = await api.get<Category[]>("/api/categories");
  return response.data;
}
