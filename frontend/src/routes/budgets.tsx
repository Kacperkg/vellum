import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "../lib/requireAuth";
import Budgets from "../pages/Budgets";

export const Route = createFileRoute("/budgets")({
  beforeLoad: requireAuth,
  component: Budgets,
});
