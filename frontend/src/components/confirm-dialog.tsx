import type { ReactNode } from "react";
import { ErrorText } from "@/components/error-text";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { usePending } from "@/hooks/use-pending";

/**
 * A modal that asks before something that can't be undone. Cancel has focus,
 * so Enter doesn't confirm by accident; Escape cancels. It stays open while
 * the action runs, and shows its error if it fails.
 */
export function ConfirmDialog({
  title,
  children,
  confirmLabel,
  pendingLabel,
  onConfirm,
  onCancel,
}: {
  title: string;
  children: ReactNode;
  confirmLabel: string;
  pendingLabel: string;
  onConfirm: () => Promise<void>;
  onCancel: () => void;
}) {
  const { pending, error, run } = usePending();
  return (
    <AlertDialog open onOpenChange={(open) => !open && !pending && onCancel()}>
      <AlertDialogContent className="w-[min(420px,calc(100vw-48px))] max-w-none sm:max-w-none">
        <AlertDialogHeader className="place-items-start text-left">
          <AlertDialogTitle className="text-[19px] leading-[1.3] font-[650] [overflow-wrap:anywhere]">{title}</AlertDialogTitle>
          <AlertDialogDescription asChild>
            <div className="flex flex-col gap-2 text-sm leading-[1.55] text-soft-foreground">{children}</div>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <ErrorText error={error} />
        <AlertDialogFooter className="mx-0 mb-0 border-0 bg-transparent p-0 pt-1 sm:justify-end">
          <AlertDialogCancel disabled={pending}>Cancel</AlertDialogCancel>
          <Button variant="destructive" onClick={() => run(onConfirm)} disabled={pending}>
            {pending ? pendingLabel : confirmLabel}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
