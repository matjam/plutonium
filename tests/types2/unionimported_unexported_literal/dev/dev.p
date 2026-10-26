package dev

type State union { Off; On int; hidden }

type R[T any] union { Ok T; Err int }
