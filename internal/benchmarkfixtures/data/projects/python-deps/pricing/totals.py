from pricing.tax import tax

def total(amount: int) -> int:
    return amount + tax(amount)
