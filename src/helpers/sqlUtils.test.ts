

import { describe, it, expect } from 'vitest';
import { isCreateOrInsert } from './sqlUtils';

describe('isCreateOrInsert', () => {
    it('should return true for CREATE TABLE queries', () => {
        expect(isCreateOrInsert('CREATE TABLE users (id INT)')).toBe(true);
        expect(isCreateOrInsert('create table users (id INT)')).toBe(true);
    });

    it('should return true for INSERT queries', () => {
        expect(isCreateOrInsert('INSERT INTO users (id) VALUES (1)')).toBe(true);
        expect(isCreateOrInsert('insert into users (id) values (1)')).toBe(true);
    });

    it('should return true for ALTER queries', () => {
        expect(isCreateOrInsert('ALTER TABLE users ADD COLUMN name VARCHAR(255)')).toBe(true);
        expect(isCreateOrInsert('alter table users add column name varchar(255)')).toBe(true);
    });

    it('should return true for DROP TABLE queries', () => {
        expect(isCreateOrInsert('DROP TABLE users')).toBe(true);
        expect(isCreateOrInsert('drop table users')).toBe(true);
    });

    it('should return true for DROP COLUMN queries', () => {
        expect(isCreateOrInsert('ALTER TABLE users DROP COLUMN name')).toBe(true);
        expect(isCreateOrInsert('alter table users drop column name')).toBe(true);
    });

    it('should return true for DROP INDEX queries', () => {
        expect(isCreateOrInsert('DROP INDEX idx_name ON users')).toBe(true);
        expect(isCreateOrInsert('drop index idx_name on users')).toBe(true);
    });

    it('should return true for CREATE DATABASE queries', () => {
        expect(isCreateOrInsert('CREATE DATABASE test_db')).toBe(true);
        expect(isCreateOrInsert('create database test_db')).toBe(true);
    });

    it('should return true for DROP DATABASE queries', () => {
        expect(isCreateOrInsert('DROP DATABASE test_db')).toBe(true);
        expect(isCreateOrInsert('drop database test_db')).toBe(true);
    });

    it('should return false for SELECT queries', () => {
        expect(isCreateOrInsert('SELECT * FROM users')).toBe(false);
        expect(isCreateOrInsert('select * from users')).toBe(false);
    });

    it('should return false for UPDATE queries', () => {
        expect(isCreateOrInsert('UPDATE users SET name = "John" WHERE id = 1')).toBe(false);
        expect(isCreateOrInsert('update users set name = "John" where id = 1')).toBe(false);
    });

    it('should return false for DELETE queries', () => {
        expect(isCreateOrInsert('DELETE FROM users WHERE id = 1')).toBe(false);
        expect(isCreateOrInsert('delete from users where id = 1')).toBe(false);
    });

    it('should return false for SELECT queries with comments', () => {
        expect(isCreateOrInsert(`
            -- Query - dim_video
            -- Create a table named 'cities' in ClickHouse
            -- select * from cities;
            -- Insert 10 sample values into the 'cities' table
            select * from cities;
        `)).toBe(false);
    });
});