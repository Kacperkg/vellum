import type { Transfer } from "@/api/types";
import NewTransactionDialog from "./NewTransactionDialog";

type DialogProps = {
  dialogOpen: boolean;
  setDialogOpen: React.Dispatch<React.SetStateAction<boolean>>;
  transfer?: Transfer;
};

export default function NewTransferDialog(props: DialogProps) {
  return <NewTransactionDialog {...props} initialType="transfer" />;
}
