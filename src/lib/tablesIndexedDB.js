import { toast } from "sonner";

const dbName = "tables_indexed_db";
const storeName = " tables_store";

function initDB() {
  return new Promise((resolve, reject) => {
    const request = window.indexedDB.open(dbName, 1);

    request.onupgradeneeded = (event) => {
      let db = event.target.result;
      if (!db.objectStoreNames.contains(storeName)) {
        db.createObjectStore(storeName, { keyPath: "id" });
      }
    };

    request.onerror = (event) => {
      reject("IndexedDB Tables could not be opened: " + event.target.errorCode);
    };

    request.onsuccess = (event) => {
      resolve(event.target.result);
    };
  });
}

function readFromDB(item) {
  return new Promise(async (resolve, reject) => {
    const db = await initDB();
    const transaction = db.transaction([storeName], "readonly");
    const store = transaction.objectStore(storeName);
    const request = store.get(item);

    request.onerror = (event) => {
      reject("Failed to retrieve data: " + event.target.errorCode);
    };

    request.onsuccess = (event) => {
      resolve(event.target.result);
    };
  });
}

async function writeToDB(item) {
  try {
    const db = await initDB();
    const transaction = db.transaction([storeName], "readwrite");
    const store = transaction.objectStore(storeName);
    item.id = item[0].database;
    store.put(item); // Just pass the whole object
    return new Promise((resolve, reject) => {
      transaction.oncomplete = () => resolve();
      transaction.onerror = (event) =>
        reject("Data could not be saved: " + event.target.errorCode);
    });
  } catch (error) {
    toast.error(error.message);
    throw error; // Rethrow or handle as necessary
  }
}

async function readAllFromDB() {
  try {
    const db = await initDB();
    const transaction = db.transaction([storeName], "readonly");
    const store = transaction.objectStore(storeName);
    const request = store.getAll();

    return new Promise((resolve, reject) => {
      request.onerror = (event) => {
        reject("Failed to retrieve data: " + event.target.errorCode);
      };

      request.onsuccess = (event) => {
        resolve(event.target.result);
      };
    });
  } catch (error) {
    toast.error(error.message);
    throw error; // Rethrow or handle as necessary
  }
}

function deleteDatabase(dbName) {
  return new Promise((resolve, reject) => {
    const request = window.indexedDB.deleteDatabase(dbName);

    request.onerror = (event) => {
      reject("Failed to delete database: " + event.target.errorCode);
    };

    request.onsuccess = () => {
      resolve();
    };
  });
}

export { initDB, readFromDB, writeToDB, readAllFromDB, deleteDatabase };
