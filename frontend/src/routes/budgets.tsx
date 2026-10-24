import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "@/features/auth";
import Budgets from "@/features/budgets/pages/Budgets";

export const Route = createFileRoute("/budgets")({
  beforeLoad: requireAuth,
  component: Budgets,
});
