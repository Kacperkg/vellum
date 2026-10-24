import { createFileRoute } from "@tanstack/react-router";
import Home from "@/features/overview/pages/Home";
import { requireAuth } from "@/features/auth";

export const Route = createFileRoute("/")({
  beforeLoad: requireAuth,
  component: Home,
});
