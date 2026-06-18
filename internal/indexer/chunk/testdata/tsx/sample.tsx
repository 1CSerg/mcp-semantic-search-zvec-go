import React from 'react';

export function Greeting(props: { name: string }) {
    return <div className="greeting">Hello, {props.name}!</div>;
}

export default function App() {
    return <Greeting name="World" />;
}
