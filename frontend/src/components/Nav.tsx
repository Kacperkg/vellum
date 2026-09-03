import Dialog from "./Dialog";
import { useState } from "react";
import Button from "./Button";
import NewTransactionDialog from "./NewTransactionDialog";

export default function Nav() {
  const [dialogOpen, setDialogOpen] = useState(false);

  return (
    <nav className="w-full border-b border-border flex gap-4 justify-end items-center pr-8 py-2">
      <Button onClick={() => setDialogOpen(true)}>+ New</Button>
      <Dialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        title="New Transaction"
        description="Log an expense or income."
        footer={
          <>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>
              Cancel
            </Button>
            <Button onClick={() => setDialogOpen(false)}>
              Add Transaction
            </Button>
          </>
        }
      >
        <NewTransactionDialog/>
      </Dialog>
      <div className="w-12 h-12 rounded-full flex border-border border justify-center items-center">
        A
      </div>
    </nav>
  );
}
