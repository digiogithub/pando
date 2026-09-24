/**
 * Single import point for icons (lucide-react).
 *
 *   import { Plus, Settings } from '@/components/ui/icons'
 *   <Plus />                       // 16px, stroke 1.75 (IconProvider defaults)
 *   <Plus size={14} />             // override per use
 *
 * Rules:
 *  - Never import from 'lucide-react' directly in feature code; add the icon
 *    here (alphabetical) and import it from this module.
 *  - Do not add new FontAwesome usages. Migrate FA icons in your area with
 *    the table below.
 *  - Icons inherit `currentColor`; colour them via the parent's text colour.
 *  - Names that clash with globals / react-router are suffixed: FileIcon,
 *    ImageIcon, LinkIcon.
 *
 * FontAwesome -> lucide equivalents (common ones):
 *   faPlus -> Plus                faTimes/faXmark -> X            faCheck -> Check
 *   faChevronDown -> ChevronDown  faChevronRight -> ChevronRight  faAngleDown -> ChevronDown
 *   faSearch/faMagnifyingGlass -> Search                          faCog/faGear -> Settings
 *   faSlidersH -> SlidersHorizontal                               faTrash/faTrashCan -> Trash2
 *   faCopy -> Copy                faPen/faPencil/faEdit -> Pencil faSave/faFloppyDisk -> Save
 *   faPaperPlane -> SendHorizontal (composer send: ArrowUp)       faPaperclip -> Paperclip
 *   faStop/faSquare -> Square     faSpinner/faCircleNotch -> LoaderCircle (or <Spinner />)
 *   faTerminal -> SquareTerminal  faFile/faFileAlt -> FileIcon/FileText faFileCode -> FileCode
 *   faFolder -> Folder            faFolderOpen -> FolderOpen      faCodeBranch -> GitBranch
 *   faComment/faComments -> MessageSquare                         faRobot -> Bot
 *   faBars -> Menu                faEllipsisH -> Ellipsis         faEllipsisV -> EllipsisVertical
 *   faExternalLinkAlt -> ExternalLink                             faInfoCircle -> Info
 *   faExclamationTriangle -> TriangleAlert                        faExclamationCircle -> CircleAlert
 *   faCheckCircle -> CircleCheck  faTimesCircle -> CircleX        faQuestionCircle -> CircleQuestionMark
 *   faSync/faRedo -> RefreshCw    faUndo -> RotateCcw             faHistory -> History
 *   faDownload -> Download        faUpload -> Upload              faEye/faEyeSlash -> Eye/EyeOff
 *   faLock/faUnlock -> Lock/LockOpen                              faKey -> Key
 *   faUser/faUsers -> User/Users  faBolt -> Zap                   faMagic/faWandMagicSparkles -> Sparkles
 *   faBrain -> Brain              faCode -> Code                  faWrench/faTools -> Wrench
 *   faPlay/faPause -> Play/Pause  faClock -> Clock                faCalendar -> Calendar
 *   faFilter -> Funnel            faServer -> Server              faDatabase -> Database
 *   faGlobe -> Globe              faLink -> LinkIcon                 faImage -> ImageIcon
 *   faMicrophone -> Mic           faBell -> Bell                  faStar -> Star
 *   faSignOutAlt -> LogOut        faExpand/faCompress -> Maximize2/Minimize2
 *   faKeyboard -> Keyboard        faLayerGroup -> Layers          faThLarge -> LayoutGrid
 *   faList -> List                faTasks/faListCheck -> ListChecks faCamera -> Camera
 *   faChartBar -> ChartColumn     faNetworkWired/faSitemap -> Network
 *   faPlug -> Plug                faPuzzlePiece -> Puzzle         faBox -> Package
 *   faShieldAlt -> Shield         faMicrochip -> Cpu              faHdd -> HardDrive
 *   faArrowLeft/Right/Up/Down -> ArrowLeft/ArrowRight/ArrowUp/ArrowDown
 *   faCodeCompare -> GitCompare   faHome -> House                 faPalette -> Palette
 *   faLanguage -> Languages       faSun/faMoon -> Sun/Moon        faDesktop -> Monitor
 *   faColumns -> Columns2         faStream/faAlignLeft -> ScrollText faCloud -> Cloud
 *   faRocket -> Rocket            faBullseye -> Target            faTachometerAlt -> Gauge
 *   faHashtag -> Hash             faAt -> AtSign                  faShareAlt -> Share2
 *   faGripVertical -> GripVertical faMinus -> Minus               faCircle -> Circle
 */

export type { LucideIcon, LucideProps } from 'lucide-react'
export { LucideProvider as IconProvider } from 'lucide-react'

/** Defaults applied app-wide through <IconProvider> in main.tsx. */
export const ICON_DEFAULTS = { size: 16, strokeWidth: 1.75 } as const

export {
  Activity,
  ArrowDown,
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  AtSign,
  Bell,
  Bookmark,
  Bot,
  Boxes,
  Brain,
  Bug,
  Calendar,
  Camera,
  ChartColumn,
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
  ChevronsUpDown,
  ChevronUp,
  Circle,
  CircleAlert,
  CircleCheck,
  CircleQuestionMark,
  CircleStop,
  CircleX,
  Clock,
  Cloud,
  CloudUpload,
  Code,
  CodeXml,
  Columns2,
  Command,
  Copy,
  CornerDownLeft,
  Cpu,
  Crosshair,
  Database,
  Diff,
  Dot,
  Download,
  Ellipsis,
  EllipsisVertical,
  ExternalLink,
  Eye,
  EyeOff,
  FastForward,
  File as FileIcon,
  FileCode,
  FilePen,
  FilePlus,
  FileText,
  Flag,
  FlaskConical,
  Folder,
  FolderOpen,
  FolderTree,
  Funnel,
  Gauge,
  Gavel,
  GitBranch,
  GitCommitHorizontal,
  GitCompare,
  Globe,
  GripVertical,
  Hammer,
  HardDrive,
  Hash,
  History,
  Hourglass,
  House,
  Image as ImageIcon,
  Info,
  Key,
  Keyboard,
  Languages,
  Layers,
  LayoutGrid,
  Lightbulb,
  Link as LinkIcon,
  List,
  ListChecks,
  ListTodo,
  LoaderCircle,
  LogIn,
  Lock,
  LockOpen,
  LogOut,
  Maximize2,
  Menu,
  MessageSquare,
  MessageSquarePlus,
  Mic,
  Minimize2,
  Minus,
  Monitor,
  Moon,
  Network,
  Package,
  Palette,
  PanelLeft,
  PanelLeftClose,
  PanelLeftOpen,
  PanelRight,
  PanelRightClose,
  Paperclip,
  Pause,
  Pencil,
  Play,
  Plug,
  Plus,
  Puzzle,
  RefreshCw,
  Rocket,
  RotateCcw,
  RotateCw,
  Save,
  ScrollText,
  Search,
  SendHorizontal,
  Server,
  Settings,
  Settings2,
  Share2,
  Shield,
  ShieldCheck,
  SlidersHorizontal,
  Sparkles,
  Split,
  Square,
  SquarePen,
  SquareTerminal,
  Star,
  Sun,
  Target,
  Terminal,
  Trash2,
  TriangleAlert,
  Trophy,
  Undo2,
  Upload,
  User,
  UserRound,
  Users,
  VenetianMask,
  Wifi,
  Workflow,
  Wrench,
  X,
  Zap,
} from 'lucide-react'
