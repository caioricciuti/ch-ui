// src/utils/indexedDB.ts
import { Tab } from "@/types/common";

const DB_NAME = "AppTabs";
const STORE_NAME = "tabs";

export const initDB = (): Promise<IDBDatabase> => {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, 1);

    request.onerror = () => reject("IndexedDB initialization failed");
    request.onsuccess = () => resolve(request.result);

    request.onupgradeneeded = (event) => {
      const db = (event.target as IDBOpenDBRequest).result;
      db.createObjectStore(STORE_NAME, { keyPath: "id" });
    };
  });
};

export const addTab = async (db: IDBDatabase, tab: Tab): Promise<void> => {
  return new Promise((resolve, reject) => {
    const transaction = db.transaction([STORE_NAME], "readwrite");
    const store = transaction.objectStore(STORE_NAME);
    const request = store.add(tab);

    request.onerror = () => reject("Error adding tab");
    request.onsuccess = () => resolve();
  });
};

export const getTabs = async (db: IDBDatabase): Promise<Tab[]> => {
  return new Promise((resolve, reject) => {
    const transaction = db.transaction([STORE_NAME], "readonly");
    const store = transaction.objectStore(STORE_NAME);
    const request = store.getAll();

    request.onerror = () => reject("Error getting tabs");
    request.onsuccess = () => resolve(request.result);
  });
};

export const updateTab = async (db: IDBDatabase, tab: Tab): Promise<void> => {
  return new Promise((resolve, reject) => {
    const transaction = db.transaction([STORE_NAME], "readwrite");
    const store = transaction.objectStore(STORE_NAME);
    const request = store.put(tab);

    request.onerror = () => reject("Error updating tab");
    request.onsuccess = () => resolve();
  });
};

export const removeTab = async (
  db: IDBDatabase,
  tabId: string
): Promise<void> => {
  return new Promise((resolve, reject) => {
    const transaction = db.transaction([STORE_NAME], "readwrite");
    const store = transaction.objectStore(STORE_NAME);
    const request = store.delete(tabId);

    request.onerror = () => reject("Error removing tab");
    request.onsuccess = () => resolve();
  });
};
