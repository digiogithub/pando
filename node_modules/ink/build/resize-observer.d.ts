import { type DOMElement } from './dom.js';
export type ResizeObserverCallback = (entries: ResizeObserverEntry[], observer: ResizeObserver) => void;
export declare class ResizeObserverEntry {
    readonly target: DOMElement;
    readonly contentRect: {
        width: number;
        height: number;
    };
    constructor(target: DOMElement, contentRect: {
        width: number;
        height: number;
    });
}
export default class ResizeObserver {
    private readonly callback;
    private readonly observedElements;
    constructor(callback: ResizeObserverCallback);
    observe(element: DOMElement): void;
    unobserve(element: DOMElement): void;
    disconnect(): void;
    internalTrigger(entries: ResizeObserverEntry[]): void;
}
