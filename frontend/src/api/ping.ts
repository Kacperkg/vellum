import { api } from "./client";

export async function ping() {
  const response = await api.get("/ping");
  return response.data;
}
