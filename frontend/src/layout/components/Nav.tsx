import { useState } from "react";
import Button from "@/components/ui/Button";
import { NewTransactionDialog } from "@/features/transactions";
import Icon from "@/components/ui/Icon";

export default function Nav() {
  const [dialogOpen, setDialogOpen] = useState(false);

  return (
    <nav className="app-topbar">
      <div className="topbar-inner">
        <Button
          className="topbar-new"
          title="Add transaction"
          aria-label="Add transaction"
          onClick={() => setDialogOpen(true)}
        >
          <Icon name="plus" /> New
        </Button>
        {dialogOpen && (
          <NewTransactionDialog
            dialogOpen={dialogOpen}
            setDialogOpen={setDialogOpen}
          />
        )}
      </div>
    </nav>
  );
}
