import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "@/features/auth";
import Goals from "@/features/goals/pages/Goals";

export const Route = createFileRoute("/goals")({
  beforeLoad: requireAuth,
  component: Goals,
});
