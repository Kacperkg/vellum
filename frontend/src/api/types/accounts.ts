export interface Account {
  id: string;
  name: string;
  balance: number;
  allocated_amount: number;
  unallocated_amount: number;
  goal_count: number;
}

export interface CreateAccountRequest {
  name: string;
}

export type UpdateAccountRequest = CreateAccountRequest;
