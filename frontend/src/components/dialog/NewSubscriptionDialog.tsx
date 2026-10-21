import NewTransactionDialog from "./NewTransactionDialog";

type DialogProps = {
  dialogOpen: boolean;
  setDialogOpen: React.Dispatch<React.SetStateAction<boolean>>;
};

export default function NewSubscriptionDialog(props: DialogProps) {
  return <NewTransactionDialog {...props} subscriptionMode />;
}
