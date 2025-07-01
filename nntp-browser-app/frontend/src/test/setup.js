// jest-dom adds custom jest matchers for asserting on DOM nodes.
// allows you to do things like:
// expect(element).toHaveTextContent(/react/i)
// learn more: https://github.com/testing-library/jest-dom
import '@testing-library/jest-dom';

// If you have global setup mocks or configurations, add them here.
// For example, mocking `fetch` if your components make API calls in useEffect during render
import { vi } from 'vitest';

// Mock global fetch
global.fetch = vi.fn();

// Clean up after each test case (optional, but good practice)
// import { cleanup } from '@testing-library/react';
// afterEach(() => {
//   cleanup();
// });
