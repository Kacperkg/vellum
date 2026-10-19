import { createFileRoute } from "@tanstack/react-router";
import Home from "../pages/Home";
import { requireAuth } from "../lib/requireAuth";

export const Route = createFileRoute("/")({
  beforeLoad: requireAuth,
  component: Home,
});
