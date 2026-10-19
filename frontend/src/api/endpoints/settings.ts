import { api } from "../client";
import type { UpdateUserSettingsRequest, UserSettings } from "../types";

const settingsPath = "/api/settings";

export async function getSettings(): Promise<UserSettings> {
  const response = await api.get<UserSettings>(settingsPath);
  return response.data;
}

export async function updateSettings(
  request: UpdateUserSettingsRequest,
): Promise<UserSettings> {
  const response = await api.put<UserSettings>(settingsPath, request);
  return response.data;
}
