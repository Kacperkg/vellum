export interface Goal {
  id: string;
  name: string;
  target_amount: number;
  saved_amount: number;
  monthly_contribution: number;
  account_id: string | null;
}

export interface CreateGoalRequest {
  name: string;
  target_amount: number;
  saved_amount: number;
  monthly_contribution: number;
  account_id?: string | null;
}

export type UpdateGoalRequest = CreateGoalRequest;
