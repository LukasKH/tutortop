import { useEffect, useEffectEvent, useState } from "react";
import { ChatService, AppEvents, useEvent, type ChatItem } from "@/lib/api";
import { isFailedTool, mergeChat, upsertChat } from "@/lib/chat";
import { objectiveOfWriter } from "@/lib/workspace";

/**
 * The workspace's conversations by thread, and which have unread messages.
 * The shown thread loads its saved transcript; new messages stream in by
 * event. Conversations stay in memory, so live (still streaming) items
 * survive switching between threads.
 */
export function useChatThreads(id: string, thread: string, { ready, visible, onError }: { ready: boolean; visible: boolean; onError: (e: unknown) => void }) {
  const [threads, setThreads] = useState<Record<string, ChatItem[]>>({});
  const [unread, setUnread] = useState<Record<string, boolean>>({});

  // Load the shown thread's transcript whenever it changes, merged with
  // anything that arrived live and isn't saved yet.
  const fail = useEffectEvent(onError);
  useEffect(() => {
    if (!ready) return;
    let current = true;
    ChatService.Chat(id, thread)
      .then((saved) => current && setThreads((t) => ({ ...t, [thread]: mergeChat(saved, t[thread] ?? []) })))
      .catch((e) => current && fail(e));
    return () => {
      current = false;
    };
  }, [id, thread, ready]);

  // What the learner can see counts as read.
  if (visible && unread[thread]) setUnread({ ...unread, [thread]: false });

  // Writers' messages aren't a chat; use-workspace.ts keeps their failures.
  useEvent(AppEvents.ChatMessage, ({ workspace, item }) => {
    const t = item.thread;
    if (workspace !== id || objectiveOfWriter(t)) return;
    setThreads((all) => ({ ...all, [t]: upsertChat(all[t] ?? [], item) }));
    if (!isFailedTool(item) && (t !== thread || !visible)) setUnread((u) => (u[t] ? u : { ...u, [t]: true }));
  });

  return { threads, unread };
}
