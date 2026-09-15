export interface Goal {
  id: string;
  name: string;
  target_amount: number;
  saved_amount: number;
  monthly_contribution: number;
}

export interface CreateGoalRequest {
  name: string;
  target_amount: number;
  saved_amount: number;
  monthly_contribution: number;
}

export type UpdateGoalRequest = CreateGoalRequest;
