import { createFileRoute } from "@tanstack/react-router";
import ResetPassword from "@/features/auth/pages/ResetPassword";

export const Route = createFileRoute("/reset-password")({
  component: ResetPassword,
});
