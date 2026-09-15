export interface Account {
  id: string;
  name: string;
}

export interface CreateAccountRequest {
  name: string;
}

export type UpdateAccountRequest = CreateAccountRequest;
