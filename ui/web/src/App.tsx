import { type Component, Show, onMount, createSignal } from "solid-js";
import { useLocation } from "@solidjs/router";
import MainLayout from "./components/layout/MainLayout";
import ChatView from "./components/chat/ChatView";
import { ServerProvider, useServer } from "./hooks/useServer";

const AppContent: Component = () => {
  const location = useLocation();
  const { connected, reconnecting, connect } = useServer();

  onMount(() => {
    connect();
  });

  return (
    <Show
      when={connected()}
      fallback={
        <div class="flex h-full w-full items-center justify-center">
          <div class="text-center">
            <Show
              when={reconnecting()}
              fallback={
                <div>
                  <p class="text-lg text-muted-foreground">
                    Unable to connect to Pando server
                  </p>
                  <button
                    onClick={() => connect()}
                    class="mt-4 rounded-lg bg-primary px-4 py-2 text-primary-foreground hover:bg-primary/90"
                  >
                    Retry Connection
                  </button>
                </div>
              }
            >
              <div class="flex items-center gap-2">
                <div class="h-4 w-4 animate-spin rounded-full border-2 border-primary border-t-transparent" />
                <p class="text-lg text-muted-foreground">Connecting to Pando...</p>
              </div>
            </Show>
          </div>
        </div>
      }
    >
      <MainLayout>
        <ChatView />
      </MainLayout>
    </Show>
  );
};

const App: Component = () => {
  return (
    <ServerProvider>
      <AppContent />
    </ServerProvider>
  );
};

export default App;
