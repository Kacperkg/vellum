import type { BaseModel } from "./common";

export interface RegisterRequest {
  first_name: string;
  last_name: string;
  email: string;
  password: string;
}

export interface LoginRequest {
  email: string;
  password: string;
}

export interface AuthResponse {
  access_token: string;
  refresh_token: string;
  user: User;
}

export interface User extends BaseModel {
  first_name: string;
  last_name: string;
  email: string;
}

export interface RefreshTokenRequest {
  refresh_token: string;
}
