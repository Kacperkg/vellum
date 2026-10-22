import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "../lib/requireAuth";
import Goals from "../pages/Goals";

export const Route = createFileRoute("/goals")({
  beforeLoad: requireAuth,
  component: Goals,
});
