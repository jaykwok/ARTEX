// Keep destructive-operation state changes together: failures must retain the
// selected task, its rows and the confirmation dialog for retry.
export async function deleteTaskRecords(
  remove: () => Promise<unknown>,
  onSuccess: () => void,
  onError: (message: string) => void,
  setBusy: (busy: boolean) => void,
): Promise<void> {
  setBusy(true);
  try {
    await remove();
    onSuccess();
  } catch (err) {
    onError(err instanceof Error ? err.message : "删除失败，请重试");
  } finally {
    setBusy(false);
  }
}
