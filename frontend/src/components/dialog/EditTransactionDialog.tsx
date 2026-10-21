import type { Transaction } from "../../api/types";
import NewTransactionDialog from "./NewTransactionDialog";
import Dialog from "./Dialog";

type DialogProps = {
  dialogOpen: boolean;
  setDialogOpen: React.Dispatch<React.SetStateAction<boolean>>;
  transaction?: Transaction;
};

export default function EditTransactionDialog({
  transaction,
  ...props
}: DialogProps) {
  if (!transaction) {
    return (
      <Dialog
        open={props.dialogOpen}
        onOpenChange={props.setDialogOpen}
        title="Edit Transaction"
        description="Choose a transaction from the Transactions page to edit it."
      />
    );
  }
  return (
    <NewTransactionDialog
      key={transaction.id}
      {...props}
      transaction={transaction}
    />
  );
}
