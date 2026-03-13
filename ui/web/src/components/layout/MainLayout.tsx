import { type Component, type JSX, Show, createSignal } from "solid-js";
import Header from "./Header";
import Sidebar from "./Sidebar";

interface MainLayoutProps {
  children: JSX.Element;
}

const MainLayout: Component<MainLayoutProps> = (props) => {
  const [sidebarOpen, setSidebarOpen] = createSignal(true);
  const [sidebarWidth, setSidebarWidth] = createSignal(280);

  return (
    <div class="flex h-full w-full flex-col overflow-hidden">
      <Header
        sidebarOpen={sidebarOpen()}
        onToggleSidebar={() => setSidebarOpen(!sidebarOpen())}
      />
      <div class="flex flex-1 overflow-hidden">
        <Show when={sidebarOpen()}>
          <Sidebar width={sidebarWidth()} />
        </Show>
        <main class="flex-1 overflow-hidden">
          {props.children}
        </main>
      </div>
    </div>
  );
};

export default MainLayout;
